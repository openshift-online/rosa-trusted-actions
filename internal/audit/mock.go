package audit

import "github.com/sirupsen/logrus"

var _ Logger = (*MockLogger)(nil)

type MockLogger struct {
	logger  *logrus.Logger
	Records []Record
}

func NewMockLogger(logger *logrus.Logger) *MockLogger {
	return &MockLogger{logger: logger}
}

func (m *MockLogger) Log(record Record) {
	m.Records = append(m.Records, record)
	m.logger.WithFields(logrus.Fields{
		"caller":      record.CallerID,
		"action":      record.Action,
		"target":      record.Target,
		"cluster_id":  record.ClusterID,
		"decision":    string(record.Decision),
		"deny_reason": record.DenyReason,
		"outcome":     string(record.Outcome),
		"error":       record.Error,
	}).Info("audit record")
}
