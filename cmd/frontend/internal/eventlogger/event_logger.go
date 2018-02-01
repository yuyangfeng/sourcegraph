package eventlogger

import (
	"fmt"
	"time"

	log15 "gopkg.in/inconshreveable/log15.v2"

	"sourcegraph.com/sourcegraph/sourcegraph/pkg/api"
	"sourcegraph.com/sourcegraph/sourcegraph/pkg/env"

	"github.com/google/uuid"
	"golang.org/x/net/context"
)

var backendEventsTrackingSiteID = "SourcegraphBackend"

// defaultLogger is a singleton for event logging from the backend
var defaultLogger = new()

// LogEvent sends a payload representing an event to either the remote
// analytics endpoint or, if not in Sourcegraph.com mode, the Server's
// internal telemetry endpoint.
//
// This method should be invoked after the frontend service has started. It is
// safe to not do so (it will just log an error), but logging the actual event
// will fail otherwise. Consider using e.g. api.InternalClient.RetryPingUntilAvailable
// to wait for the frontend to start.
//
// Note: This does not block since it creates a new goroutine.
func LogEvent(userEmail string, eventLabel string, eventProperties map[string]string) {
	go func() {
		err := defaultLogger.logEvent(userEmail, eventLabel, eventProperties)
		if err != nil {
			log15.Warn("eventlogger.LogEvent failed", "event", eventLabel, "error", err)
		}
	}()
}

// eventLogger represents a connection to a remote URL for sending
// event logs, with environment and user context
type eventLogger struct {
	env, url string
	ctx      context.Context
}

// new returns a new EventLogger client
func new() *eventLogger {
	environment := "production"
	if env.Version == "dev" {
		environment = "development"
	}
	return &eventLogger{
		env: environment,
	}
}

// newPayload generates a new Payload struct for a provided event
// in the context of the EventLogger client
func (logger *eventLogger) newPayload(userEmail string, event *Event) *Payload {
	userInfo := &UserInfo{
		DomainUserID: "sourcegraph-backend-anonymous",
	}
	if userEmail != "" {
		userInfo = &UserInfo{
			DomainUserID: uuid.New().String(),
			Email:        userEmail,
		}
	}
	return &Payload{
		DeviceInfo: &DeviceInfo{
			Platform:         "Web",
			TrackerNamespace: "sg",
		},
		Events: []*Event{
			event,
		},
		Header: &Header{
			SiteID: backendEventsTrackingSiteID,
			Env:    logger.env,
		},
		BatchInfo: &BatchInfo{
			BatchID:     uuid.New().String(),
			TotalEvents: 1,
			ServerTime:  fmt.Sprintf("%d", time.Now().UTC().Unix()*1000),
		},
		UserInfo: userInfo,
	}
}

// logEvent sends a payload representing some user event to the InternalClient telemetry API
func (logger *eventLogger) logEvent(userEmail string, eventLabel string, eventProperties map[string]string) error {
	event := &Event{
		Type:            eventLabel,
		EventID:         uuid.New().String(),
		ClientTimestamp: time.Now().UTC().Unix() * 1000,
		Context: &Context{
			EventLabel: eventLabel,
			Backend:    eventProperties,
		},
	}
	payload := logger.newPayload(userEmail, event)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	return api.InternalClient.LogTelemetry(ctx, logger.env, payload)
}
