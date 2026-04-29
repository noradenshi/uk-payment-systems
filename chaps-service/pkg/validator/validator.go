package validator

import (
	"fmt"
	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/xsd"
)

type ISOValidator struct {
	schema *xsd.Schema
}

type ValidatorRegistry struct {
	validators map[string]*ISOValidator
}

func NewValidatorRegistry() *ValidatorRegistry {
	return &ValidatorRegistry{
		validators: make(map[string]*ISOValidator),
	}
}

// Register adds a new XSD version to our registry
func (r *ValidatorRegistry) Register(version string, xsdPath string) error {
	s, err := xsd.ParseFromFile(xsdPath)
	if err != nil {
		return fmt.Errorf("failed to parse %s: %w", xsdPath, err)
	}
	r.validators[version] = &ISOValidator{schema: s}
	return nil
}

// ValidateByVersion finds the right schema and validates the XML
func (r *ValidatorRegistry) ValidateByVersion(version string, xmlData []byte) error {
	v, ok := r.validators[version]
	if !ok {
		return fmt.Errorf("unsupported XSD version: %s", version)
	}

	doc, err := libxml2.Parse(xmlData)
	if err != nil {
		return err
	}
	defer doc.Free()

	err = v.schema.Validate(doc)
    if err != nil {
        return fmt.Errorf("XSD Error: %v", err)
    }
    return nil
}
