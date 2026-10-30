package handler

import (
	"encoding/json"
	"testing"

	"github.com/pennsieve/app-deploy-service/service/mappers"
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testCFF = `cff-version: 1.2.0
message: Please cite this app as below.
title: CytoNorm normalization
version: 2.1.0
doi: https://doi.org/10.5281/zenodo.1234567
date-released: 2026-10-01
authors:
  - given-names: Katrien
    family-names: Quintelier
    orcid: https://orcid.org/0000-0002-1825-0097
    affiliation: VIB-UGent
  - name: Pennsieve Team
preferred-citation:
  type: article
  title: "CytoNorm: A Normalization Algorithm for Cytometry Data"
  authors:
    - given-names: Sofie
      family-names: Van Gassen
  journal: Cytometry A
  year: 2020
  doi: 10.1002/cyto.a.23904
references:
  - type: dataset
    title: Spectral flow cytometry of stimulated PBMCs
    doi: doi:10.5281/ZENODO.15388817
  - type: software
    title: FlowKit
    url: https://github.com/whitews/FlowKit
`

func TestAppCitation(t *testing.T) {
	got, problems := appCitation([]byte(testCFF), true, map[string]string{
		"https://doi.org/10.5281/zenodo.15388817": "IsDerivedFrom",
	})
	assert.Empty(t, problems)
	require.NotNil(t, got)

	assert.Equal(t, "CytoNorm normalization", got.Title)
	assert.Equal(t, "2.1.0", got.Version)
	assert.Equal(t, "10.5281/zenodo.1234567", got.DOI)
	assert.Equal(t, "2026-10-01", got.DateReleased)
	assert.Equal(t, []store_dynamodb.CitationAuthor{
		{Name: "Katrien Quintelier", GivenNames: "Katrien", FamilyNames: "Quintelier", Orcid: "0000-0002-1825-0097", Affiliation: "VIB-UGent"},
		{Name: "Pennsieve Team"},
	}, got.Authors)

	preferred := store_dynamodb.RelatedWork{DOI: "10.1002/cyto.a.23904", RelationType: "IsDescribedBy",
		Title: "CytoNorm: A Normalization Algorithm for Cytometry Data", Authors: []string{"Sofie Van Gassen"},
		Year: 2020, Journal: "Cytometry A", Source: "preferred-citation"}
	require.NotNil(t, got.PreferredCitation)
	assert.Equal(t, preferred, *got.PreferredCitation)
	assert.Equal(t, []store_dynamodb.RelatedWork{
		preferred,
		{DOI: "10.5281/zenodo.15388817", RelationType: "IsDerivedFrom", Title: "Spectral flow cytometry of stimulated PBMCs", Source: "references"},
		{RelationType: "References", Title: "FlowKit", URL: "https://github.com/whitews/FlowKit", Source: "references"},
	}, got.RelatedWorks)
}

func TestAppCitation_NoFile(t *testing.T) {
	got, problems := appCitation(nil, false, nil)
	assert.Nil(t, got)
	assert.Empty(t, problems, "a repository without CITATION.cff is fine")

	got, problems = appCitation(nil, false, map[string]string{"10.1038/nbt.4314": "References"})
	assert.Nil(t, got)
	assert.Equal(t, []string{"/application/relations/10.1038/nbt.4314: not a DOI listed in CITATION.cff (preferred-citation or references)"}, problems)
}

func TestAppCitation_Problems(t *testing.T) {
	got, problems := appCitation([]byte(testCFF), true, map[string]string{
		"10.9999/not.listed":      "Requires",
		"10.1002/cyto.a.23904":    "Cites",      // the schema reports the type
		"flowsom":                 "References", // and the key
		"10.5281/zenodo.15388817": "IsSourceOf",
	})
	require.NotNil(t, got)
	assert.Equal(t, []string{"/application/relations/10.9999/not.listed: not a DOI listed in CITATION.cff (preferred-citation or references)"}, problems)
	assert.Equal(t, "IsDescribedBy", got.RelatedWorks[0].RelationType, "an invalid type leaves the default")
	assert.Equal(t, "IsSourceOf", got.RelatedWorks[1].RelationType)

	got, problems = appCitation([]byte("title: [unclosed"), true, map[string]string{"10.1038/nbt.4314": "References"})
	assert.Nil(t, got, "an invalid CITATION.cff isn't stored")
	require.Len(t, problems, 1, "nor are its relations checked")
	assert.Contains(t, problems[0], "CITATION.cff is not valid YAML")

	got, problems = appCitation([]byte("cff-version: 1.2.0\ntitle: x\nreferences:\n  - doi: 10.1/x\n"), true, nil)
	require.NotNil(t, got, "a parseable file missing fields is stored")
	assert.Equal(t, []string{"CITATION.cff: message: required", "CITATION.cff: authors: required", "CITATION.cff: references[0].title: required"}, problems)
}

func TestAppConfig_Relations(t *testing.T) {
	c, err := parseAppConfig([]byte("application:\n  name: a\n  relations:\n    10.5281/zenodo.15388817: IsDerivedFrom\n"))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"10.5281/zenodo.15388817": "IsDerivedFrom"}, c.Application.Relations)
	assert.Empty(t, manifestProblems([]byte("application:\n  name: a\n  relations:\n    10.5281/zenodo.15388817: IsDerivedFrom\n")))
}

func TestSyncFilesIncludeCitation(t *testing.T) {
	t.Setenv("CONTENT_SYNC_FILES", "")
	assert.Contains(t, getSyncFiles(), "CITATION.cff")
}

// The JSON the store endpoints return for a version's citation.
func TestAppCitation_JSON(t *testing.T) {
	c, _ := appCitation([]byte(testCFF), true, nil)
	b, err := json.Marshal(mappers.AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "v", Citation: c}))
	require.NoError(t, err)
	var v map[string]any
	require.NoError(t, json.Unmarshal(b, &v))
	cite := v["citation"].(map[string]any)
	assert.Equal(t, "CytoNorm normalization", cite["title"])
	assert.Equal(t, "0000-0002-1825-0097", cite["authors"].([]any)[0].(map[string]any)["orcid"])
	assert.Equal(t, "IsDescribedBy", cite["preferredCitation"].(map[string]any)["relationType"])
	works := cite["relatedWorks"].([]any)
	assert.Len(t, works, 3)
	flowkit := works[2].(map[string]any)
	assert.Equal(t, "", flowkit["doi"], "doi is always there, empty without one")
	assert.Equal(t, "references", flowkit["source"])

	b, err = json.Marshal(mappers.AppStoreVersionToModel(store_dynamodb.AppStoreVersion{Uuid: "v"}))
	require.NoError(t, err)
	assert.NotContains(t, string(b), "citation")
}
