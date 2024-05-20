package parser

import (
	"context"
	"encoding/json"
	"os"
)

type Parser interface {
	Run(context.Context) (Output, error)
}

type OutputParser struct {
	FileLocation string
}

func NewOutputParser(location string) Parser {
	return &OutputParser{FileLocation: location}
}

func (o *OutputParser) Run(ctx context.Context) (Output, error) {
	data, err := os.ReadFile(o.FileLocation)
	if err != nil {
		return Output{}, err
	}

	var outputs Output
	err = json.Unmarshal(data, &outputs)
	if err != nil {
		return Output{}, err
	}

	return outputs, nil
}
