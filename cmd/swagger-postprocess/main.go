package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	spec, err := loadSpec("docs/swagger.json")
	if err != nil {
		return err
	}

	normalizeSpec(spec)

	jsonBytes, err := json.MarshalIndent(spec, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal spec: %w", err)
	}
	jsonBytes = append(jsonBytes, '\n')

	if err := os.WriteFile("docs/swagger.json", jsonBytes, 0o644); err != nil {
		return fmt.Errorf("write docs/swagger.json: %w", err)
	}
	// JSON is valid YAML, so keep swagger.yaml in sync deterministically.
	if err := os.WriteFile("docs/swagger.yaml", jsonBytes, 0o644); err != nil {
		return fmt.Errorf("write docs/swagger.yaml: %w", err)
	}
	if err := rewriteDocTemplate("docs/docs.go", jsonBytes); err != nil {
		return err
	}

	return nil
}

func loadSpec(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var spec map[string]any
	if err := json.Unmarshal(b, &spec); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", path, err)
	}
	return spec, nil
}

func normalizeSpec(spec map[string]any) {
	spec["openapi"] = "3.0.3"
	normalizeServers(spec)

	components, _ := spec["components"].(map[string]any)
	securitySchemes, _ := components["securitySchemes"].(map[string]any)
	if scheme, ok := securitySchemes["bearerauth"]; ok {
		securitySchemes["BearerAuth"] = scheme
		delete(securitySchemes, "bearerauth")
	}

	paths, _ := spec["paths"].(map[string]any)
	for pathKey, pathValue := range paths {
		operations, ok := pathValue.(map[string]any)
		if !ok {
			continue
		}
		for _, operationValue := range operations {
			operation, ok := operationValue.(map[string]any)
			if !ok {
				continue
			}
			normalizeSecurity(operation)
			normalizeRequestBody(operation)
			if strings.HasSuffix(pathKey, "/sign/evm") {
				injectEVMDiscriminator(operation)
			}
		}
	}
}

// injectEVMDiscriminator adds a `discriminator` block to the EVM sign
// request `oneOf` so OpenAPI codegen produces a sealed-class-style typed
// client. Swag does not emit `discriminator` natively from struct tags.
// Matching is keyed on `propertyName: "type"`; the mapping points at the
// three variant schemas already produced by swag.
func injectEVMDiscriminator(operation map[string]any) {
	requestBody, ok := operation["requestBody"].(map[string]any)
	if !ok {
		return
	}
	content, ok := requestBody["content"].(map[string]any)
	if !ok {
		return
	}
	for _, mt := range content {
		mediaType, ok := mt.(map[string]any)
		if !ok {
			continue
		}
		schema, ok := mediaType["schema"].(map[string]any)
		if !ok {
			continue
		}
		if _, exists := schema["oneOf"]; !exists {
			continue
		}
		schema["discriminator"] = map[string]any{
			"propertyName": "type",
			"mapping": map[string]any{
				"raw_tx":           "#/components/schemas/types.EVMSignRawTxRequest",
				"personal_message": "#/components/schemas/types.EVMSignPersonalMessageRequest",
				"eip712_digest":    "#/components/schemas/types.EVMSignEIP712Request",
			},
		}
	}
}

func normalizeServers(spec map[string]any) {
	serversAny, ok := spec["servers"]
	if !ok {
		return
	}
	servers, ok := serversAny.([]any)
	if !ok {
		return
	}
	for _, serverAny := range servers {
		server, ok := serverAny.(map[string]any)
		if !ok {
			continue
		}
		url, _ := server["url"].(string)
		if url == "" || strings.Contains(url, "://") || strings.HasPrefix(url, "/") {
			continue
		}
		server["url"] = "http://" + url
	}
}

func normalizeSecurity(operation map[string]any) {
	securityAny, ok := operation["security"]
	if !ok {
		return
	}
	security, ok := securityAny.([]any)
	if !ok {
		return
	}
	if len(security) == 1 {
		if item, ok := security[0].(map[string]any); ok {
			if scopes, ok := item[""]; ok {
				if scopeList, ok := scopes.([]any); ok && len(scopeList) == 0 {
					operation["security"] = []any{}
					return
				}
			}
		}
	}
	for _, item := range security {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if scopes, ok := entry["bearerauth"]; ok {
			entry["BearerAuth"] = scopes
			delete(entry, "bearerauth")
		}
	}
}

func normalizeRequestBody(operation map[string]any) {
	requestBodyAny, ok := operation["requestBody"]
	if !ok {
		return
	}
	requestBody, ok := requestBodyAny.(map[string]any)
	if !ok {
		return
	}
	contentAny, ok := requestBody["content"]
	if !ok {
		return
	}
	content, ok := contentAny.(map[string]any)
	if !ok {
		return
	}
	for _, mediaTypeAny := range content {
		mediaType, ok := mediaTypeAny.(map[string]any)
		if !ok {
			continue
		}
		schemaAny, ok := mediaType["schema"]
		if !ok {
			continue
		}
		schema, ok := schemaAny.(map[string]any)
		if !ok {
			continue
		}
		normalizeSchema(schema)
	}
}

func normalizeSchema(schema map[string]any) {
	oneOfAny, ok := schema["oneOf"]
	if !ok {
		return
	}
	oneOf, ok := oneOfAny.([]any)
	if !ok {
		return
	}
	filtered := make([]any, 0, len(oneOf))
	for _, item := range oneOf {
		entry, ok := item.(map[string]any)
		if ok && len(entry) == 1 {
			if t, ok := entry["type"].(string); ok && t == "object" {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	switch len(filtered) {
	case 0:
		delete(schema, "oneOf")
	case 1:
		only, ok := filtered[0].(map[string]any)
		if !ok {
			schema["oneOf"] = filtered
			return
		}
		for key := range schema {
			delete(schema, key)
		}
		for key, value := range only {
			schema[key] = value
		}
	default:
		schema["oneOf"] = filtered
	}
}

func rewriteDocTemplate(path string, spec []byte) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	data := string(content)
	startMarker := "const docTemplate = `"
	start := strings.Index(data, startMarker)
	if start == -1 {
		return fmt.Errorf("doc template start marker not found in %s", path)
	}
	contentStart := start + len(startMarker)
	endRel := strings.Index(data[contentStart:], "`\n")
	if endRel == -1 {
		return fmt.Errorf("doc template end marker not found in %s", path)
	}
	end := contentStart + endRel

	var builder strings.Builder
	builder.Grow(len(data) + len(spec))
	builder.WriteString(data[:contentStart])
	builder.Write(spec)
	builder.WriteString(data[end:])

	if err := os.WriteFile(path, []byte(builder.String()), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
