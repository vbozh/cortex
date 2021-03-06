package client

import (
	"fmt"
	"reflect"
	"sort"
	"testing"
	"unsafe"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/textparse"
	"github.com/stretchr/testify/assert"
	"github.com/thanos-io/thanos/pkg/testutil"
)

func TestQueryRequest(t *testing.T) {
	from, to := model.Time(int64(0)), model.Time(int64(10))
	matchers := []*labels.Matcher{}
	matcher1, err := labels.NewMatcher(labels.MatchEqual, "foo", "1")
	if err != nil {
		t.Fatal(err)
	}
	matchers = append(matchers, matcher1)

	matcher2, err := labels.NewMatcher(labels.MatchNotEqual, "bar", "2")
	if err != nil {
		t.Fatal(err)
	}
	matchers = append(matchers, matcher2)

	matcher3, err := labels.NewMatcher(labels.MatchRegexp, "baz", "3")
	if err != nil {
		t.Fatal(err)
	}
	matchers = append(matchers, matcher3)

	matcher4, err := labels.NewMatcher(labels.MatchNotRegexp, "bop", "4")
	if err != nil {
		t.Fatal(err)
	}
	matchers = append(matchers, matcher4)

	req, err := ToQueryRequest(from, to, matchers)
	if err != nil {
		t.Fatal(err)
	}

	haveFrom, haveTo, haveMatchers, err := FromQueryRequest(req)
	if err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(haveFrom, from) {
		t.Fatalf("Bad from FromQueryRequest(ToQueryRequest) round trip")
	}
	if !reflect.DeepEqual(haveTo, to) {
		t.Fatalf("Bad to FromQueryRequest(ToQueryRequest) round trip")
	}
	if !reflect.DeepEqual(haveMatchers, matchers) {
		t.Fatalf("Bad have FromQueryRequest(ToQueryRequest) round trip - %v != %v", haveMatchers, matchers)
	}
}

func buildTestMatrix(numSeries int, samplesPerSeries int, offset int) model.Matrix {
	m := make(model.Matrix, 0, numSeries)
	for i := 0; i < numSeries; i++ {
		ss := model.SampleStream{
			Metric: model.Metric{
				model.MetricNameLabel: model.LabelValue(fmt.Sprintf("testmetric_%d", i)),
				model.JobLabel:        "testjob",
			},
			Values: make([]model.SamplePair, 0, samplesPerSeries),
		}
		for j := 0; j < samplesPerSeries; j++ {
			ss.Values = append(ss.Values, model.SamplePair{
				Timestamp: model.Time(i + j + offset),
				Value:     model.SampleValue(i + j + offset),
			})
		}
		m = append(m, &ss)
	}
	sort.Sort(m)
	return m
}

func TestMetricMetadataToMetricTypeToMetricType(t *testing.T) {
	tc := []struct {
		desc     string
		input    MetricMetadata_MetricType
		expected textparse.MetricType
	}{
		{
			desc:     "with a single-word metric",
			input:    COUNTER,
			expected: textparse.MetricTypeCounter,
		},
		{
			desc:     "with a two-word metric",
			input:    STATESET,
			expected: textparse.MetricTypeStateset,
		},
		{
			desc:     "with an unknown metric",
			input:    MetricMetadata_MetricType(100),
			expected: textparse.MetricTypeUnknown,
		},
	}

	for _, tt := range tc {
		t.Run(tt.desc, func(t *testing.T) {
			m := MetricMetadataMetricTypeToMetricType(tt.input)
			testutil.Equals(t, tt.expected, m)
		})
	}
}

func TestFromLabelAdaptersToLabels(t *testing.T) {
	input := []LabelAdapter{{Name: "hello", Value: "world"}}
	expected := labels.Labels{labels.Label{Name: "hello", Value: "world"}}
	actual := FromLabelAdaptersToLabels(input)

	assert.Equal(t, expected, actual)

	// All strings must NOT be copied.
	assert.Equal(t, uintptr(unsafe.Pointer(&input[0].Name)), uintptr(unsafe.Pointer(&actual[0].Name)))
	assert.Equal(t, uintptr(unsafe.Pointer(&input[0].Value)), uintptr(unsafe.Pointer(&actual[0].Value)))
}

func TestFromLabelAdaptersToLabelsWithCopy(t *testing.T) {
	input := []LabelAdapter{{Name: "hello", Value: "world"}}
	expected := labels.Labels{labels.Label{Name: "hello", Value: "world"}}
	actual := FromLabelAdaptersToLabelsWithCopy(input)

	assert.Equal(t, expected, actual)

	// All strings must be copied.
	assert.NotEqual(t, uintptr(unsafe.Pointer(&input[0].Name)), uintptr(unsafe.Pointer(&actual[0].Name)))
	assert.NotEqual(t, uintptr(unsafe.Pointer(&input[0].Value)), uintptr(unsafe.Pointer(&actual[0].Value)))
}

func TestQueryResponse(t *testing.T) {
	want := buildTestMatrix(10, 10, 10)
	have := FromQueryResponse(ToQueryResponse(want))
	if !reflect.DeepEqual(have, want) {
		t.Fatalf("Bad FromQueryResponse(ToQueryResponse) round trip")
	}

}

// This test shows label sets with same fingerprints, and also shows how to easily create new collisions
// (by adding "_" or "A" label with specific values, see below).
func TestFingerprintCollisions(t *testing.T) {
	// "8yn0iYCKYHlIj4-BwPqk" and "GReLUrM4wMqfg9yzV3KQ" have same FNV-1a hash.
	// If we use it as a single label name (for labels that have same value), we get colliding labels.
	c1 := labels.FromStrings("8yn0iYCKYHlIj4-BwPqk", "hello")
	c2 := labels.FromStrings("GReLUrM4wMqfg9yzV3KQ", "hello")
	verifyCollision(t, true, c1, c2)

	// Adding _="ypfajYg2lsv" or _="KiqbryhzUpn" respectively to most metrics will produce collision.
	// It's because "_\xffypfajYg2lsv" and "_\xffKiqbryhzUpn" have same FNV-1a hash, and "_" label is sorted before
	// most other labels (except labels starting with upper-case letter)

	const _label1 = "ypfajYg2lsv"
	const _label2 = "KiqbryhzUpn"

	metric := labels.NewBuilder(labels.FromStrings("__name__", "logs"))
	c1 = metric.Set("_", _label1).Labels()
	c2 = metric.Set("_", _label2).Labels()
	verifyCollision(t, true, c1, c2)

	metric = labels.NewBuilder(labels.FromStrings("__name__", "up", "instance", "hello"))
	c1 = metric.Set("_", _label1).Labels()
	c2 = metric.Set("_", _label2).Labels()
	verifyCollision(t, true, c1, c2)

	// here it breaks, because "Z" label is sorted before "_" label.
	metric = labels.NewBuilder(labels.FromStrings("__name__", "up", "Z", "hello"))
	c1 = metric.Set("_", _label1).Labels()
	c2 = metric.Set("_", _label2).Labels()
	verifyCollision(t, false, c1, c2)

	// A="K6sjsNNczPl" and A="cswpLMIZpwt" label has similar property.
	// (Again, because "A\xffK6sjsNNczPl" and "A\xffcswpLMIZpwt" have same FNV-1a hash)
	// This time, "A" is the smallest possible label name, and is always sorted first.

	const Alabel1 = "K6sjsNNczPl"
	const Alabel2 = "cswpLMIZpwt"

	metric = labels.NewBuilder(labels.FromStrings("__name__", "up", "Z", "hello"))
	c1 = metric.Set("A", Alabel1).Labels()
	c2 = metric.Set("A", Alabel2).Labels()
	verifyCollision(t, true, c1, c2)

	// Adding the same suffix to the "A" label also works.
	metric = labels.NewBuilder(labels.FromStrings("__name__", "up", "Z", "hello"))
	c1 = metric.Set("A", Alabel1+"suffix").Labels()
	c2 = metric.Set("A", Alabel2+"suffix").Labels()
	verifyCollision(t, true, c1, c2)
}

func verifyCollision(t *testing.T, collision bool, ls1 labels.Labels, ls2 labels.Labels) {
	if collision && Fingerprint(ls1) != Fingerprint(ls2) {
		t.Errorf("expected same fingerprints for %v (%016x) and %v (%016x)", ls1.String(), Fingerprint(ls1), ls2.String(), Fingerprint(ls2))
	} else if !collision && Fingerprint(ls1) == Fingerprint(ls2) {
		t.Errorf("expected different fingerprints for %v (%016x) and %v (%016x)", ls1.String(), Fingerprint(ls1), ls2.String(), Fingerprint(ls2))
	}
}
