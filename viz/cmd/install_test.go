package cmd

import (
	"bytes"
	"fmt"
	"testing"

	charts "github.com/linkerd/linkerd2/pkg/charts"
)

func TestRender(t *testing.T) {

	// pin values that are changed by render functions on each test run
	defaultValues := map[string]interface{}{
		"tap": map[string]interface{}{
			"keyPEM":   "test-tap-key-pem",
			"crtPEM":   "test-tap-crt-pem",
			"caBundle": "test-tap-ca-bundle",
		},
		"tapInjector": map[string]interface{}{
			"keyPEM":   "test-tap-key-pem",
			"crtPEM":   "test-tap-crt-pem",
			"caBundle": "test-tap-ca-bundle",
		},
	}

	proxyResources := map[string]interface{}{
		"proxy": map[string]interface{}{
			"resources": map[string]interface{}{
				"cpu": map[string]interface{}{
					"request": "500m",
					"limit":   "100m",
				},
				"memory": map[string]interface{}{
					"request": "20Mi",
					"limit":   "250Mi",
				},
			},
		},
	}

	testCases := []struct {
		values         map[string]interface{}
		goldenFileName string
	}{
		{
			nil,
			"install_default.golden",
		},
		{
			map[string]interface{}{
				"prometheus":    map[string]interface{}{"enabled": false},
				"prometheusUrl": "external-prom.com",
			},
			"install_prometheus_disabled.golden",
		},
		{
			map[string]interface{}{
				"prometheus": proxyResources,
				"tap":        proxyResources,
				"grafana":    proxyResources,
				"dashboard":  proxyResources,
			},
			"install_proxy_resources.golden",
		},
	}

	for i, tc := range testCases {
		tc := tc // pin
		t.Run(fmt.Sprintf("%d: %s", i, tc.goldenFileName), func(t *testing.T) {
			var buf bytes.Buffer
			// Merge overrides with default
			if err := render(&buf, charts.MergeMaps(defaultValues, tc.values)); err != nil {
				t.Fatalf("Failed to render templates: %v", err)
			}
			testDataDiffer.DiffTestdata(t, tc.goldenFileName, buf.String())
		})
	}
}
