package jetstream

import (
	"context"
	"encoding/json"
	"fmt"
	"sidekick/domain"
	"strconv"
	"time"

	"github.com/nats-io/nats.go/jetstream"
)

// Ensure Streamer implements FlowActionStreamer
var _ domain.FlowActionStreamer = (*Streamer)(nil)

func (s *Streamer) AddFlowActionChange(ctx context.Context, flowAction domain.FlowAction) error {
	data, err := json.Marshal(flowAction)
	if err != nil {
		return fmt.Errorf("failed to marshal flow action: %w", err)
	}

	subject := fmt.Sprintf("flow_actions.changes.%s.%s", flowAction.WorkspaceId, flowAction.FlowId)
	_, err = s.js.Publish(ctx, subject, data)
	if err != nil {
		return fmt.Errorf("failed to publish flow action change: %w", err)
	}

	return nil
}

func (s *Streamer) StreamFlowActionChanges(ctx context.Context, workspaceId, flowId, streamMessageStartId string) (<-chan domain.FlowAction, <-chan error) {
	// default to starting from the start of the stream for flow action changes
	if streamMessageStartId == "" {
		streamMessageStartId = "0"
	}

	flowActionChan := make(chan domain.FlowAction)
	errChan := make(chan error, 1)

	go func() {
		defer close(flowActionChan)
		defer close(errChan)

		subject := fmt.Sprintf("flow_actions.changes.%s.%s", workspaceId, flowId)

		var deliveryPolicy jetstream.DeliverPolicy
		var startSeq uint64
		if streamMessageStartId == "0" {
			deliveryPolicy = jetstream.DeliverAllPolicy
		} else if streamMessageStartId == "$" {
			deliveryPolicy = jetstream.DeliverNewPolicy
		} else {
			deliveryPolicy = jetstream.DeliverByStartSequencePolicy
			var err error
			startSeq, err = strconv.ParseUint(streamMessageStartId, 10, 64)
			if err != nil {
				errChan <- fmt.Errorf("invalid stream message start id: %w", err)
				return
			}
		}

		consumer, err := s.js.OrderedConsumer(ctx, PersistentStreamName, jetstream.OrderedConsumerConfig{
			FilterSubjects:    []string{subject},
			InactiveThreshold: 5 * time.Minute,
			DeliverPolicy:     deliveryPolicy,
			OptStartSeq:       startSeq,
		})
		if err != nil {
			errChan <- fmt.Errorf("failed to create consumer: %w", err)
			return
		}

		var consContext jetstream.ConsumeContext
		consContext, err = consumer.Consume(func(msg jetstream.Msg) {
			var flowAction domain.FlowAction
			if err := json.Unmarshal(msg.Data(), &flowAction); err != nil {
				errChan <- fmt.Errorf("failed to unmarshal flow action: %w", err)
				return
			}
			select {
			case flowActionChan <- flowAction:
				if flowAction.Id == "end" {
					fmt.Printf("Received end message\n")
					consContext.Stop()
				}
				msg.Ack()
			case <-ctx.Done():
				return
			}
		})
		if err != nil {
			errChan <- fmt.Errorf("failed to create consume context: %w", err)
			return
		}

		select {
		case <-consContext.Closed():
		case <-ctx.Done():
			consContext.Stop()
			<-consContext.Closed()
		}
	}()

	return flowActionChan, errChan
}
