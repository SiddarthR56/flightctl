// Package v1alpha1 provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/oapi-codegen/oapi-codegen/v2 version v2.3.0 DO NOT EDIT.
package v1alpha1

import (
	"bytes"
	"compress/gzip"
	"encoding/base64"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/getkin/kin-openapi/openapi3"
)

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"8gEA",
}

// GetSwagger returns the content of the embedded swagger specification file
// or error if failed to decode
func decodeSpec() ([]byte, error) {
	zipped, err := base64.StdEncoding.DecodeString(strings.Join(swaggerSpec, ""))
	if err != nil {
		return nil, fmt.Errorf("error base64 decoding spec: %w", err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(zipped))
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}
	var buf bytes.Buffer
	_, err = buf.ReadFrom(zr)
	if err != nil {
		return nil, fmt.Errorf("error decompressing spec: %w", err)
	}

	return buf.Bytes(), nil
}

var rawSpec = decodeSpecCached()

// a naive cached of a decoded swagger spec
func decodeSpecCached() func() ([]byte, error) {
	data, err := decodeSpec()
	return func() ([]byte, error) {
		return data, err
	}
}

// Constructs a synthetic filesystem for resolving external references when loading openapi specifications.
func PathToRawSpec(pathToFile string) map[string]func() ([]byte, error) {
	res := make(map[string]func() ([]byte, error))
	if len(pathToFile) > 0 {
		res[pathToFile] = rawSpec
	}

	return res
}

// GetSwagger returns the Swagger specification corresponding to the generated code
// in this file. The external references of Swagger specification are resolved.
// The logic of resolving external references is tightly connected to "import-mapping" feature.
// Externally referenced files must be embedded in the corresponding golang packages.
// Urls can be supported but this task was out of the scope.
func GetSwagger() (swagger *openapi3.T, err error) {
	resolvePath := PathToRawSpec("")

	loader := openapi3.NewLoader()
	loader.IsExternalRefsAllowed = true
	loader.ReadFromURIFunc = func(loader *openapi3.Loader, url *url.URL) ([]byte, error) {
		pathToFile := url.String()
		pathToFile = path.Clean(pathToFile)
		getSpec, ok := resolvePath[pathToFile]
		if !ok {
			err1 := fmt.Errorf("path not found: %s", pathToFile)
			return nil, err1
		}
		return getSpec()
	}
	var specData []byte
	specData, err = rawSpec()
	if err != nil {
		return
	}
	swagger, err = loader.LoadFromData(specData)
	if err != nil {
		return
	}
	return
}
