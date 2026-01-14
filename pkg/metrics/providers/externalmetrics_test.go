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
	"testing"
	"time"
	"strings"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/inf.v0"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	//fake "k8s.io/client-go/kubernetes/fake"

	flaggerv1 "github.com/fluxcd/flagger/pkg/apis/flagger/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stesting "k8s.io/client-go/testing"
	emv1beta1 "k8s.io/metrics/pkg/apis/external_metrics/v1beta1"
	fakeemc "k8s.io/metrics/pkg/client/external_metrics/fake"
)

const (
	testMetricName          = "myMetric"
	testMetricNamespace     = "default"
	testMetricServerAddress = "https://external-metrics.default.svc.cluster.local"
	testQuery               = "default/myMetric?labelSelector=label1%3Dvalue1"
)

var (
	testMetricLabels       = [...]string{"label1"}
	testMetricLabelsValues = [...]string{"value1"}
	// 11111e-4 = 1.1111
	testMetricValue        = resource.NewDecimalQuantity(*inf.NewDec(11111, 4), resource.DecimalSI)
)

func TestExternalMetrics_NewProvider(t *testing.T) {
	t.Run("Custom token", func(t *testing.T) {
		mtp := flaggerv1.MetricTemplateProvider{
			Address:            testMetricServerAddress,
			InsecureSkipVerify: false,
		}
		creds := map[string][]byte{
			"token": []byte("test-token"),
		}

		// Should be OK
		emp, err := NewExternalMetricsProvider("100s", mtp, creds)
		require.NoError(t, err)
		assert.Equal(t, 5*time.Second, emp.timeout)
	})
	t.Run("In cluster, automatic token", func(t *testing.T) {
		// TODO: Mock API server / secret file injection ?
	})
}

func TestExternalMetrics_ParseQuery(t *testing.T) {
	// TODO: assess relevance of moving these to table-driven tests
	t.Run("General case", func(t *testing.T) {
		metricNamespace, metricName, labelSelector, err := parseExternalMetricsQuery(testQuery)
		require.NoError(t, err)
		assert.Equal(t, testMetricNamespace, metricNamespace)
		assert.Equal(t, testMetricName, metricName)
		assert.Equal(t, labels.Set{testMetricLabels[0]: testMetricLabelsValues[0]}.AsSelector(), labelSelector)
	})
	t.Run("OK without labelSelector", func(t *testing.T) {
		trimmed := testQuery[:strings.Index(testQuery, "?")]
		metricNamespace, metricName, labelSelector, err := parseExternalMetricsQuery(trimmed)
		require.NoError(t, err)
		assert.Equal(t, testMetricNamespace, metricNamespace)
		assert.Equal(t, testMetricName, metricName)
		assert.Equal(t, labels.Everything(), labelSelector)
	})
	t.Run("Missing metric name", func(t *testing.T) {
		invalidQueries := []string{
			"namespaceonly/",
			"/",
			"",
		}
		for _, iq := range invalidQueries {
			_, _, _, err := parseExternalMetricsQuery(iq)
			require.Error(t, err)
		}
	})
	t.Run("No namespace uses default", func(t *testing.T) {
		ns, _, _, err := parseExternalMetricsQuery("/metric_only")
		require.NoError(t, err)
		assert.Equal(t, "default", ns)
	})
}

func TestExternalMetrics_RunQuery(t *testing.T) {
	fakeExternalMetricsClient := fakeemc.FakeExternalMetricsClient{}
	fakeExternalMetricsClient.Fake.AddReactor("list", "*", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, &emv1beta1.ExternalMetricValueList{
			Items: []emv1beta1.ExternalMetricValue{
				{
					MetricName: testMetricName,
					Value:      *testMetricValue,
					MetricLabels: map[string]string{
						testMetricLabels[0]: testMetricLabelsValues[0],
					},
					Timestamp: metav1.Now(),
				},
			},
		}, nil
	})

	emp := &ExternalMetricsProvider{
		timeout: 5 * time.Second,
		client:  &fakeExternalMetricsClient,
	}

	tests := []struct {
		name  string
		query string
	}{
		{
			name:  "Full query with label selector",
			query: testQuery,
		},
		{
			name:  "Namespace and metric only",
			query: "namespace/" + testMetricName,
		},
		{
			name:  "Metric only, default namespace",
			query: testMetricName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := emp.RunQuery(tt.query)
			require.NoError(t, err)
			assert.Equal(t, testMetricValue.AsApproximateFloat64(), f)
		})
	}
}

func TestExternalMetrics_IsOnline(t *testing.T) {
	emp := &ExternalMetricsProvider{
		timeout: 5 * time.Second,
		client:  &fakeemc.FakeExternalMetricsClient{},
	}

	online, err := emp.IsOnline()
	require.NoError(t, err)
	assert.True(t, online)
}
