package main

import "testing"

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
