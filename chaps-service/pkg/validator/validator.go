package validator

import (
	"errors"
	"fmt"
	"strings"

	"github.com/lestrrat-go/libxml2"
	"github.com/lestrrat-go/libxml2/types"
	"github.com/lestrrat-go/libxml2/xpath"
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

func (r *ValidatorRegistry) RegisterW(version string) error {
	return r.Register(version, "xsd/"+version)
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

	if err := v.schema.Validate(doc); err != nil {
		return fmt.Errorf("XSD Error [%s]: %v", version, err)
	}

	return nil
}

func (r *ValidatorRegistry) ValidateWrapped(payload []byte) ([]byte, string, error) {
	// 1. One-pass validation: Validate the entire BizMsg envelope
	// This replaces extractAndValidateBAH for the initial structural check.
	if err := r.ValidateByVersion("chaps_wrapper", payload); err != nil {
		return nil, "", fmt.Errorf("envelope validation failed: %w", err)
	}

	doc, err := libxml2.Parse(payload)
	if err != nil {
		return nil, "", err
	}
	defer doc.Free()

	// 2. Peek the MsgDefIdr to know which message type we are dealing with
	msgDefIdr, err := r.peekMsgDefIdr(doc)
	if err != nil {
		return nil, "", err
	}

	// 3. Extract the Document for the Application Layer
	ctx, err := xpath.NewContext(doc)
	if err != nil {
		return nil, msgDefIdr, err
	}
	defer ctx.Free()

	res, err := ctx.Find("//*[local-name()='Document']")
	if err != nil {
		return nil, msgDefIdr, err
	}

	nodes := res.NodeList()
	if len(nodes) == 0 {
		return nil, msgDefIdr, errors.New("could not find Document tag")
	}

	// We still convert to string/bytes for the Unmarshaler to process
	docBytes := []byte(nodes[0].String())

	return docBytes, msgDefIdr, nil
}

// Helper to peek MsgDefIdr from a validated document
func (r *ValidatorRegistry) peekMsgDefIdr(doc types.Document) (string, error) {
	ctx, err := xpath.NewContext(doc)
	if err != nil {
		return "", err
	}
	defer ctx.Free()

	res, err := ctx.Find("//*[local-name()='MsgDefIdr']")
	if err != nil {
		return "", err
	}
	nodes := res.NodeList()
	if len(nodes) == 0 {
		return "", errors.New("MsgDefIdr not found in validated envelope")
	}

	return strings.TrimSpace(nodes[0].NodeValue()), nil
}
