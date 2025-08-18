package periodic

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/sirupsen/logrus"
)

const (
	DefaultChannelBufferSize = 10
)

// ChannelManager manages a buffered channel for periodic task scheduling
type ChannelManager struct {
	taskChannel chan PeriodicTaskReference
	log         logrus.FieldLogger
	mu          sync.RWMutex
	closed      bool
}

type ChannelManagerConfig struct {
	Log               logrus.FieldLogger
	ChannelBufferSize int
}

func NewChannelManager(config ChannelManagerConfig) (*ChannelManager, error) {
	if config.Log == nil {
		return nil, fmt.Errorf("log is required")
	}

	bufferSize := config.ChannelBufferSize
	if bufferSize <= 0 {
		bufferSize = DefaultChannelBufferSize
	}

	return &ChannelManager{
		taskChannel: make(chan PeriodicTaskReference, bufferSize),
		log:         config.Log,
		closed:      false,
	}, nil
}

func (cm *ChannelManager) PublishTask(ctx context.Context, taskRef PeriodicTaskReference) error {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if cm.closed {
		cm.log.Warn("Attempted to publish task to closed channel manager")
		return errors.New("channel manager is closed")
	}

	select {
	case cm.taskChannel <- taskRef:
		cm.log.Debugf("Published task %s for organization %s", taskRef.Type, taskRef.OrgID)
		return nil
	default:
		// Channel is full, fail immediately
		cm.log.Debugf("Failed to publish task %s for organization %s: channel is full", taskRef.Type, taskRef.OrgID)
		return errors.New("channel is full")
	}
}

func (cm *ChannelManager) Tasks() <-chan PeriodicTaskReference {
	return cm.taskChannel
}

func (cm *ChannelManager) Close() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if !cm.closed {
		cm.log.Info("Closing channel manager")
		close(cm.taskChannel)
		cm.closed = true
	}
}
