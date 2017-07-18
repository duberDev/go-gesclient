package internal

import (
	"errors"
	"fmt"
	"github.com/jdextraze/go-gesclient/client"
	"github.com/jdextraze/go-gesclient/operations"
	"github.com/jdextraze/go-gesclient/subscriptions"
	"github.com/jdextraze/go-gesclient/tasks"
	"github.com/satori/go.uuid"
)

type connection struct {
	connectionSettings *client.ConnectionSettings
	clusterSettings    *client.ClusterSettings
	name               string
	endpointDiscoverer EndpointDiscoverer
	handler            ConnectionLogicHandler
}

func NewConnection(
	settings *client.ConnectionSettings,
	clusterSettings *client.ClusterSettings,
	endpointDiscoverer EndpointDiscoverer,
	name string,
) client.Connection {
	if settings == nil {
		panic("settings is nil")
	}
	if endpointDiscoverer == nil {
		panic("endpointDiscoverer is nil")
	}

	if name == "" {
		name = fmt.Sprintf("ES-%s", uuid.NewV4())
	}

	c := &connection{
		connectionSettings: settings,
		clusterSettings:    clusterSettings,
		endpointDiscoverer: endpointDiscoverer,
		name:               name,
	}
	c.handler = NewConnectionLogicHandler(c, settings)
	return c
}

func (c *connection) Name() string {
	return c.name
}

func (c *connection) ConnectAsync() *tasks.Task {
	source := tasks.NewCompletionSource()
	c.handler.EnqueueMessage(newStartConnectionMessage(source, c.endpointDiscoverer))
	return source.Task()
}

func (c *connection) Close() error {
	return c.handler.EnqueueMessage(newCloseConnectionMessage("Connection close requested by client.", nil))
}

func (c *connection) DeleteStreamAsync(
	stream string,
	expectedVersion int,
	hardDelete bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		return nil, errors.New("stream must be present")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewDeleteStream(source, stream, expectedVersion, hardDelete, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) AppendToStreamAsync(
	stream string,
	expectedVersion int,
	events []*client.EventData,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		panic("stream is empty")
	}
	if events == nil {
		panic("events is nil")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewAppendToStream(source, c.connectionSettings.RequireMaster(), stream, expectedVersion, events,
		userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) ReadEventAsync(
	stream string,
	eventNumber int,
	resolveTos bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		return nil, errors.New("stream must be present")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewReadEvent(source, stream, eventNumber, resolveTos, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) ReadStreamEventsForwardAsync(
	stream string,
	start int,
	max int,
	resolveLinkTos bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		return nil, errors.New("stream must be present")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewReadStreamEventsForward(source, stream, start, max, resolveLinkTos,
		c.Settings().RequireMaster(), userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) ReadStreamEventsBackwardAsync(
	stream string,
	start int,
	max int,
	resolveLinkTos bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		return nil, errors.New("stream must be present")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewReadStreamEventsBackward(source, stream, start, max, resolveLinkTos,
		c.Settings().RequireMaster(), userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) ReadAllEventsForwardAsync(
	position *client.Position,
	max int,
	resolveTos bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if position == nil {
		panic("position is nil")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewReadAllEventsForward(source, position, max, resolveTos, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) ReadAllEventsBackwardAsync(
	position *client.Position,
	max int,
	resolveTos bool,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if position == nil {
		panic("position is nil")
	}
	source := tasks.NewCompletionSource()
	op := operations.NewReadAllEventsBackward(source, position, max, resolveTos, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) SubscribeToStreamAsync(
	stream string,
	resolveLinkTos bool,
	eventAppeared client.EventAppearedHandler,
	subscriptionDropped client.SubscriptionDroppedHandler,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if stream == "" {
		panic("stream is empty")
	}
	if eventAppeared == nil {
		panic("eventAppeared is nil")
	}
	source := tasks.NewCompletionSource()
	return source.Task(), c.handler.EnqueueMessage(&startSubscriptionMessage{
		source:              source,
		streamId:            stream,
		resolveLinkTos:      resolveLinkTos,
		userCredentials:     userCredentials,
		eventAppeared:       eventAppeared,
		subscriptionDropped: subscriptionDropped,
		maxRetries:          c.connectionSettings.MaxReconnections(),
		timeout:             c.connectionSettings.OperationTimeout(),
	})
}

func (c *connection) SubscribeToAllAsync(
	resolveLinkTos bool,
	eventAppeared client.EventAppearedHandler,
	subscriptionDropped client.SubscriptionDroppedHandler,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	if eventAppeared == nil {
		panic("eventAppeared is nil")
	}
	source := tasks.NewCompletionSource()
	return source.Task(), c.handler.EnqueueMessage(&startSubscriptionMessage{
		source:              source,
		streamId:            "",
		resolveLinkTos:      resolveLinkTos,
		userCredentials:     userCredentials,
		eventAppeared:       eventAppeared,
		subscriptionDropped: subscriptionDropped,
		maxRetries:          c.Settings().MaxRetries(),
		timeout:             c.Settings().OperationTimeout(),
	})
}

func (c *connection) SubscribeToStreamFrom(
	stream string,
	lastCheckpoint *int,
	settings *client.CatchUpSubscriptionSettings,
	eventAppeared client.CatchUpEventAppearedHandler,
	liveProcessingStarted client.LiveProcessingStartedHandler,
	subscriptionDropped client.CatchUpSubscriptionDroppedHandler,
	userCredentials *client.UserCredentials,
) (client.CatchUpSubscription, error) {
	sub := subscriptions.NewStreamCatchUpSubscription(c, stream, lastCheckpoint, userCredentials, eventAppeared,
		liveProcessingStarted, subscriptionDropped, settings)
	sub.Start()
	return sub, nil
}

func (c *connection) SubscribeToAllFrom(
	lastCheckpoint *client.Position,
	settings *client.CatchUpSubscriptionSettings,
	eventAppeared client.CatchUpEventAppearedHandler,
	liveProcessingStarted client.LiveProcessingStartedHandler,
	subscriptionDropped client.CatchUpSubscriptionDroppedHandler,
	userCredentials *client.UserCredentials,
) (client.CatchUpSubscription, error) {
	sub := subscriptions.NewAllCatchUpSubscription(c, lastCheckpoint, userCredentials, eventAppeared,
		liveProcessingStarted, subscriptionDropped, settings)
	sub.Start()
	return sub, nil
}

func (c *connection) CreatePersistentSubscriptionAsync(
	stream string,
	groupName string,
	settings *client.PersistentSubscriptionSettings,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	source := tasks.NewCompletionSource()
	op := operations.NewCreatePersistentSubscription(source, stream, groupName, settings, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) UpdatePersistentSubscriptionAsync(
	stream string,
	groupName string,
	settings *client.PersistentSubscriptionSettings,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	source := tasks.NewCompletionSource()
	op := operations.NewUpdatePersistentSubscription(source, stream, groupName, settings, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) DeletePersistentSubscriptionAsync(
	stream string,
	groupName string,
	userCredentials *client.UserCredentials,
) (*tasks.Task, error) {
	source := tasks.NewCompletionSource()
	op := operations.NewDeletePersistentSubscription(source, stream, groupName, userCredentials)
	return source.Task(), c.enqueueOperation(op)
}

func (c *connection) enqueueOperation(op client.Operation) error {
	return c.handler.EnqueueMessage(&startOperationMessage{
		operation:  op,
		maxRetries: c.connectionSettings.MaxReconnections(),
		timeout:    c.connectionSettings.OperationTimeout(),
	})
}

func (c *connection) Settings() *client.ConnectionSettings {
	return c.connectionSettings
}

func (c *connection) Connected() client.EventHandlers { return c.handler.Connected() }

func (c *connection) Disconnected() client.EventHandlers { return c.handler.Disconnected() }

func (c *connection) Reconnecting() client.EventHandlers { return c.handler.Reconnecting() }

func (c *connection) Closed() client.EventHandlers { return c.handler.Closed() }

func (c *connection) ErrorOccurred() client.EventHandlers { return c.handler.ErrorOccurred() }

func (c *connection) AuthenticationFailed() client.EventHandlers {
	return c.handler.AuthenticationFailed()
}