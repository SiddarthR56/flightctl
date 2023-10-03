// Package v1alpha1 provides primitives to interact with the openapi HTTP API.
//
// Code generated by github.com/deepmap/oapi-codegen version v1.15.0 DO NOT EDIT.
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
