package memberagentarc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	. "github.com/onsi/gomega"

	"sigs.k8s.io/yaml"
)

// TestHelmChartTemplatesRenderValidYAML tests that Helm chart templates render
// to valid YAML with maximal configuration values that activate all conditional paths.
func TestHelmChartTemplatesRenderValidYAML(t *testing.T) {
	g := NewWithT(t)
	// Maximal configuration to activate all conditional template paths
	values := map[string]interface{}{
		"memberagent": map[string]interface{}{
			"repository": "mcr.microsoft.com/aks/fleet/member-agent",
			"tag":        "v1.0.0",
		},
		"mcscontrollermanager": map[string]interface{}{
			"repository": "mcr.microsoft.com/aks/fleet/mcs-controller-manager",
			"tag":        "v1.0.0",
		},
		"membernetcontrollermanager": map[string]interface{}{
			"repository": "mcr.microsoft.com/aks/fleet/member-net-controller-manager",
			"tag":        "v1.0.0",
		},
		"refreshtoken": map[string]interface{}{
			"repository": "mcr.microsoft.com/aks/fleet/refresh-token",
			"tag":        "v1.0.0",
		},
		"crdinstaller": map[string]interface{}{
			"enabled":      true,
			"repository":   "mcr.microsoft.com/aks/fleet/crd-installer",
			"tag":          "v1.0.0",
			"logVerbosity": 2,
		},
		"logVerbosity": 5,
		"namespace":    "fleet-system",
		"config": map[string]interface{}{
			"scope":             "https://test.scope",
			"hubURL":            "https://hub.example.com",
			"memberClusterName": "test-cluster",
			"hubCA":             "test-ca-cert",
		},
		"enableV1Beta1APIs":           true,
		"enableTrafficManagerFeature": true,
		"enableNetworkingFeatures":    true,
		"propertyProvider":            "azure",
		"Azure": map[string]interface{}{
			"proxySettings": map[string]interface{}{
				"isProxyEnabled": true,
				"httpProxy":      "http://proxy.example.com:8080",
				"httpsProxy":     "https://proxy.example.com:8443",
				"noProxy":        "localhost,127.0.0.1",
				"proxyCert":      "test-proxy-cert",
			},
			"Identity": map[string]interface{}{
				"MSIAdapterYaml": "image: mcr.microsoft.com/aks/msi-adapter:v1.0.0\nresources:\n  limits:\n    cpu: 100m",
			},
			"Extension": map[string]interface{}{
				"Name": "fleet-member-extension",
			},
		},
	}

	// Template context matching Helm's structure
	context := map[string]interface{}{
		"Values": values,
		"Release": map[string]interface{}{
			"Name":      "test-release",
			"Namespace": "fleet-system",
		},
		"Chart": map[string]interface{}{
			"Name":    "arc-member-cluster-agents",
			"Version": "1.0.0",
		},
	}

	templatesDir := "templates"
	entries, err := os.ReadDir(templatesDir)
	g.Expect(err).ToNot(HaveOccurred(), "Failed to read templates directory")

	validatedCount := 0
	for _, entry := range entries {
		// Skip directories and helper templates
		if entry.IsDir() || strings.HasPrefix(entry.Name(), "_") {
			continue
		}

		templatePath := filepath.Join(templatesDir, entry.Name())
		templateBytes, err := os.ReadFile(templatePath)
		g.Expect(err).ToNot(HaveOccurred(), "Failed to read template %s", entry.Name())

		// Parse and render the Go template
		tmpl := template.New(entry.Name()).Funcs(helmFuncMap())
		tmpl, err = tmpl.Parse(string(templateBytes))
		g.Expect(err).ToNot(HaveOccurred(), "Failed to parse template %s", entry.Name())

		var rendered strings.Builder
		err = tmpl.Execute(&rendered, context)
		g.Expect(err).ToNot(HaveOccurred(), "Failed to render template %s. Err :s", entry.Name(), err)

		renderedContent := strings.TrimSpace(rendered.String())
		g.Expect(renderedContent).ToNot(BeEmpty(), "Rendered template %s is empty", entry.Name())

		// Validate each YAML document in multi-doc files
		docs := strings.Split(renderedContent, "\n---\n")
		validDocsCount := 0
		for i, doc := range docs {
			doc = strings.TrimSpace(doc)
			if doc == "" {
				continue
			}

			var obj interface{}
			err := yaml.Unmarshal([]byte(doc), &obj)
			g.Expect(err).ToNot(HaveOccurred(), "Template %s doc %d is invalid YAML: %v\nContent:\n%s",
				entry.Name(), i+1, err, doc)
			validDocsCount++
		}

		validatedCount++
	}

	g.Expect(validatedCount).ToNot(BeZero(), "No templates were validated")
}

// helmFuncMap returns template functions that mimic Helm's template functions
func helmFuncMap() template.FuncMap {
	return template.FuncMap{
		"nindent": func(spaces int, s string) string {
			indent := strings.Repeat(" ", spaces)
			lines := strings.Split(s, "\n")
			var result []string
			for i, line := range lines {
				if i == 0 {
					result = append(result, "\n"+indent+line)
				} else {
					result = append(result, indent+line)
				}
			}
			return strings.Join(result, "\n")
		},
		"quote": func(s interface{}) string {
			return `"` + toString(s) + `"`
		},
		"b64enc": func(s string) string {
			// Simple mock - just return the string for testing purposes
			return s
		},
		"include": func(name string, data interface{}) string {
			// Simple mock - return empty string for testing
			return ""
		},
	}
}

func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
