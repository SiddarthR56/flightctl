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

	externalRef0 "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/getkin/kin-openapi/openapi3"
)

// Base64 encoded, gzipped, json marshaled Swagger object
var swaggerSpec = []string{

	"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",
	"/HgAAA==",
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

	pathPrefix := path.Dir(pathToFile)

	for rawPath, rawFunc := range externalRef0.PathToRawSpec(path.Join(pathPrefix, "../openapi.yaml")) {
		if _, ok := res[rawPath]; ok {
			// it is not possible to compare functions in golang, so always overwrite the old value
		}
		res[rawPath] = rawFunc
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
