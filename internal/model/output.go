package model

// Output represents a parsed output reference
type Output struct {
	// Type returns the type of the reference
	// e.g. docker::image-name -> docker
	Type string

	// Identifier returns the identifying part of the reference (without type)
	// e.g. docker::image-name -> image-name
	Identifier string
}

func NewOutput(typeName, identifier string) Output {
	return Output{
		Type:       typeName,
		Identifier: identifier,
	}
}

func (o Output) String() string {
	return o.Type + "::" + o.Identifier
}

func (o Output) IsSet() bool {
	return o.Identifier != ""
}

func (o Output) IsFile() bool {
	return o.Type == "file"
}
