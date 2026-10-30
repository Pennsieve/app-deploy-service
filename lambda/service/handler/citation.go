package handler

import (
	"github.com/pennsieve/app-deploy-service/service/store_dynamodb"
	"github.com/pennsieve/pennsieve-go-core/pkg/citation"
)

// citationFile is the repository's citation metadata (Citation File Format
// 1.2.0): its authors, how to cite it, and the works it builds on.
const citationFile = "CITATION.cff"

// appCitation reads a version's CITATION.cff (cff; have is false when the
// repository has none) with app.yml's application.relations applied. It
// returns what to store, nil when there's no file or it can't be parsed, and
// the problems to record with the version's manifest problems: an invalid
// CITATION.cff, and a relation for a DOI it doesn't list. A relation with an
// invalid type or key is left to the schema (manifestschema), which reports
// it. None of these refuse a release.
func appCitation(cff []byte, have bool, relations map[string]string) (*store_dynamodb.AppCitation, []string) {
	var problems []string
	var f *citation.File
	if have {
		parsed, err := citation.Parse(cff)
		if err != nil {
			return nil, []string{err.Error()}
		}
		f = parsed
		for _, p := range citation.Validate(f) {
			problems = append(problems, citationFile+": "+p)
		}
	}

	// Only relations the schema accepts are checked against the file, so
	// each problem is reported once.
	wellFormed := map[string]string{}
	for doi, t := range relations {
		if citation.IsValidRelationType(t) && citation.IsDOI(citation.NormalizeDOI(doi)) {
			wellFormed[doi] = t
		}
	}
	for _, p := range citation.CheckRelations(f, wellFormed) {
		problems = append(problems, "/application/relations/"+p)
	}

	if f == nil {
		return nil, problems
	}
	return storedCitation(f, citation.RelatedWorks(f, wellFormed)), problems
}

func storedCitation(f *citation.File, works []citation.RelatedWork) *store_dynamodb.AppCitation {
	c := &store_dynamodb.AppCitation{
		Title:        f.Title,
		Version:      f.Version,
		DOI:          citation.NormalizeDOI(f.DOI),
		DateReleased: f.DateReleased,
	}
	for _, a := range f.Authors {
		name := a.DisplayName()
		if name == "" {
			continue
		}
		c.Authors = append(c.Authors, store_dynamodb.CitationAuthor{
			Name:        name,
			GivenNames:  a.GivenNames,
			FamilyNames: a.FamilyNames,
			Orcid:       a.ORCIDID(),
			Affiliation: a.Affiliation,
		})
	}
	for _, w := range works {
		stored := store_dynamodb.RelatedWork{
			DOI:          w.DOI,
			RelationType: w.RelationType,
			Title:        w.Title,
			Authors:      w.Authors,
			Year:         w.Year,
			Journal:      w.Journal,
			URL:          w.URL,
			Source:       w.Source,
		}
		if w.Source == citation.SourcePreferredCitation && c.PreferredCitation == nil {
			preferred := stored
			c.PreferredCitation = &preferred
		}
		c.RelatedWorks = append(c.RelatedWorks, stored)
	}
	return c
}
