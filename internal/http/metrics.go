package http

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// filteredSubjectsTotal counts NATS internal subjects filtered from ServiceAccount annotations
	filteredSubjectsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "nats_auth_filtered_internal_subjects_total",
			Help: "Total number of NATS internal subjects filtered from ServiceAccount annotations",
		},
		[]string{"namespace", "serviceaccount", "annotation", "pattern"},
	)
)

// IncrementFilteredSubjects increments the counter for a filtered internal subject
func IncrementFilteredSubjects(namespace, serviceaccount, annotation, subject string) {
	pattern := "_INBOX"
	if strings.HasPrefix(subject, "_REPLY") {
		pattern = "_REPLY"
	}

	filteredSubjectsTotal.WithLabelValues(
		namespace,
		serviceaccount,
		annotation,
		pattern,
	).Inc()
}
