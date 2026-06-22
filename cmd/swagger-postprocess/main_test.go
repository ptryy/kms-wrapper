package main

import "testing"

func TestRenameSchemaPrefix_KeysAndRefs(t *testing.T) {
	old := "github_com_ryan-truong_kms-wrapper_pkg_types"
	spec := map[string]any{
		"components": map[string]any{
			"schemas": map[string]any{
				old + ".KeyInfo": map[string]any{"type": "object"},
				old + ".EVMSignRawTxRequest": map[string]any{
					"properties": map[string]any{
						"nested": map[string]any{"$ref": "#/components/schemas/" + old + ".KeyInfo"},
					},
				},
			},
		},
		"paths": map[string]any{
			"/v1/sign/evm": map[string]any{
				"post": map[string]any{
					"requestBody": map[string]any{"content": map[string]any{
						"application/json": map[string]any{"schema": map[string]any{
							"oneOf": []any{map[string]any{"$ref": "#/components/schemas/" + old + ".EVMSignRawTxRequest"}},
						}},
					}},
				},
			},
		},
	}

	renameSchemaPrefix(spec, old, "kms-wrapper_pkg_types")

	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["kms-wrapper_pkg_types.KeyInfo"]; !ok {
		t.Fatal("KeyInfo key not renamed")
	}
	if _, ok := schemas[old+".KeyInfo"]; ok {
		t.Fatal("old KeyInfo key still present")
	}
	nestedRef := schemas["kms-wrapper_pkg_types.EVMSignRawTxRequest"].(map[string]any)["properties"].(map[string]any)["nested"].(map[string]any)["$ref"]
	if nestedRef != "#/components/schemas/kms-wrapper_pkg_types.KeyInfo" {
		t.Fatalf("nested $ref not rewritten: %v", nestedRef)
	}
	oneOfRef := spec["paths"].(map[string]any)["/v1/sign/evm"].(map[string]any)["post"].(map[string]any)["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["oneOf"].([]any)[0].(map[string]any)["$ref"]
	if oneOfRef != "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest" {
		t.Fatalf("oneOf $ref not rewritten: %v", oneOfRef)
	}
}

func TestInjectEVMDiscriminator_UsesShortPrefix(t *testing.T) {
	op := map[string]any{"requestBody": map[string]any{"content": map[string]any{
		"application/json": map[string]any{"schema": map[string]any{"oneOf": []any{}}},
	}}}
	injectEVMDiscriminator(op)
	mapping := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/json"].(map[string]any)["schema"].(map[string]any)["discriminator"].(map[string]any)["mapping"].(map[string]any)
	if mapping["raw_tx"] != "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest" {
		t.Fatalf("raw_tx mapping wrong: %v", mapping["raw_tx"])
	}
	if mapping["personal_message"] != "#/components/schemas/kms-wrapper_pkg_types.EVMSignPersonalMessageRequest" {
		t.Fatalf("personal_message mapping wrong: %v", mapping["personal_message"])
	}
	if mapping["eip712_digest"] != "#/components/schemas/kms-wrapper_pkg_types.EVMSignEIP712Request" {
		t.Fatalf("eip712_digest mapping wrong: %v", mapping["eip712_digest"])
	}
}

func TestValidateSpecRefs_DanglingMappingFails(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{"schemas": map[string]any{
			"kms-wrapper_pkg_types.EVMSignRawTxRequest": map[string]any{},
		}},
		"paths": map[string]any{"/v1/sign/evm": map[string]any{"post": map[string]any{
			"requestBody": map[string]any{"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"discriminator": map[string]any{"mapping": map[string]any{
						"raw_tx": "#/components/schemas/kms-wrapper_pkg_types.MISSING",
					}},
				}},
			}},
		}}},
	}
	if err := validateSpecRefs(spec); err == nil {
		t.Fatal("expected dangling-ref error")
	}
}

func TestValidateSpecRefs_AllResolveOK(t *testing.T) {
	spec := map[string]any{
		"components": map[string]any{"schemas": map[string]any{
			"kms-wrapper_pkg_types.EVMSignRawTxRequest": map[string]any{},
		}},
		"paths": map[string]any{"/v1/sign/evm": map[string]any{"post": map[string]any{
			"requestBody": map[string]any{"content": map[string]any{
				"application/json": map[string]any{"schema": map[string]any{
					"discriminator": map[string]any{"mapping": map[string]any{
						"raw_tx": "#/components/schemas/kms-wrapper_pkg_types.EVMSignRawTxRequest",
					}},
				}},
			}},
		}}},
	}
	if err := validateSpecRefs(spec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeServers(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "adds scheme to host", url: "localhost:8080/", want: "http://localhost:8080/"},
		{name: "keeps http scheme", url: "http://localhost:8080/", want: "http://localhost:8080/"},
		{name: "keeps https scheme", url: "https://api.example.com/", want: "https://api.example.com/"},
		{name: "keeps empty url", url: "", want: ""},
		{name: "keeps relative url", url: "/v1", want: "/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := map[string]any{
				"servers": []any{
					map[string]any{"url": tt.url},
				},
			}
			normalizeSpec(spec)

			servers, ok := spec["servers"].([]any)
			if !ok || len(servers) != 1 {
				t.Fatalf("servers shape mismatch: %#v", spec["servers"])
			}
			server, ok := servers[0].(map[string]any)
			if !ok {
				t.Fatalf("server entry shape mismatch: %#v", servers[0])
			}
			got, _ := server["url"].(string)
			if got != tt.want {
				t.Fatalf("url mismatch: got %q want %q", got, tt.want)
			}
		})
	}
}
