/*
Copyright 2020 The Flux authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package providers

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	
	rest "k8s.io/client-go/rest"
	flaggerv1 "github.com/fluxcd/flagger/pkg/apis/flagger/v1beta1"
	externalmetrics_client "k8s.io/metrics/pkg/client/external_metrics"
	labels "k8s.io/apimachinery/pkg/labels"
)

// ExternalMetricsProvider fetches metrics from an ExternalMetricsProvider.
type ExternalMetricsProvider struct {
	metricServiceEndpoint string

	timeout time.Duration
	client  *externalmetrics_client.ExternalMetricsClient
}

// NewExternalMetricsProvider takes a canary spec, a provider spec, and
// returns a client ready to execute queries against the Service
func NewExternalMetricsProvider(metricInterval string,
	provider flaggerv1.MetricTemplateProvider,
	credentials map[string][]byte) (*ExternalMetricsProvider, error) {

	if provider.Address == "" {
		return nil, fmt.Errorf("the Url of the external metric service must be provided")
	}
	
	client, err := externalmetrics_client.NewForConfig(
		&rest.Config{
			Host: provider.Address,
			TLSClientConfig: rest.TLSClientConfig{
				Insecure: provider.InsecureSkipVerify,
			},
		},
	)

	if err != nil {
		return nil, fmt.Errorf("error creating external metric client: %w", err)
	}

	emp := ExternalMetricsProvider{
		timeout:               5 * time.Second,
		client:                &client,
	}

	return &emp, nil
}

// RunQuery retrieves the ExternalMetricValue from the ExternalMetricsProvider.metricServiceUrl
// and returns the first result as a float64
func (p *ExternalMetricsProvider) RunQuery(query string) (float64, error) {
	// The Provider interface only allows a plain string query so decode it
	namespace, metricName, selector, err := parseExternalMetricsQuery(query)
	if err != nil {
		return 0, fmt.Errorf("error parsing metric query: %w", err)
	}

	// Read metrics from external metrics API
	nm := (*p.client).NamespacedMetrics(namespace)
	s, err := labels.Parse(selector)
	if err != nil {
		return 0, fmt.Errorf("error parsing label selector: %w", err)
	}

	metricsList, err := nm.List(metricName, s)
	if len(metricsList.Items) < 1 {
		return 0, fmt.Errorf("no external metrics found: %w", ErrNoValuesFound)
	}

	// We accept to ignore extra metrics if more that one matches
	vs := metricsList.Items[0].Value.AsApproximateFloat64()

	return vs, nil
}

// IsOnline tests that the External metric API is reachable by looking for dummy metrics
// If we don't get a network error, we assume the service is online
func (p *ExternalMetricsProvider) IsOnline() (bool, error) {
    nm := (*p.client).NamespacedMetrics("kube-system")
    _, err := nm.List("dummy-metric", labels.Everything())
    
    if err != nil {
        return false, fmt.Errorf("external metrics service unavailable: %w", err)
    }
    return true, nil
}

func parseExternalMetricsQuery(query string) (namespace, metricName, labelSelector string, err error) {
    parts := strings.SplitN(query, "?", 2)
    pathPart := parts[0]
    
    if len(parts) > 1 {
        queryParams, err := url.ParseQuery(parts[1])
        if err != nil {
            return "", "", "", fmt.Errorf("error parsing query parameters: %w", err)
        }
        labelSelector = queryParams.Get("labelSelector")
    }
    
    pathSegments := strings.SplitN(pathPart, "/", 2)
    if len(pathSegments) != 2 {
        return "", "", "", fmt.Errorf("invalid query format: expected <namespace>/<metricName>, got %s", pathPart)
    }
    
    namespace = pathSegments[0]
    metricName = pathSegments[1]
    
    if namespace == "" || metricName == "" {
        return "", "", "", fmt.Errorf("namespace and metricName cannot be empty")
    }
    
    return namespace, metricName, labelSelector, nil
}