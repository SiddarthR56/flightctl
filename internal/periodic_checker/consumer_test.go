package periodic

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type mockPeriodicTaskExecutor struct {
	mu               sync.Mutex
	executeCallCount int
	executeCallArgs  []executeCallArgs
}

type executeCallArgs struct {
	log   logrus.FieldLogger
	orgID uuid.UUID
}

func (m *mockPeriodicTaskExecutor) Execute(ctx context.Context, log logrus.FieldLogger, orgID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executeCallCount++
	m.executeCallArgs = append(m.executeCallArgs, executeCallArgs{log: log, orgID: orgID})
}

func (m *mockPeriodicTaskExecutor) GetExecuteCallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.executeCallCount
}

func (m *mockPeriodicTaskExecutor) GetExecuteCallArgs() []executeCallArgs {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]executeCallArgs, len(m.executeCallArgs))
	copy(result, m.executeCallArgs)
	return result
}

// panicPeriodicTaskExecutor is a mock executor that always panics
type panicPeriodicTaskExecutor struct {
	mu               sync.Mutex
	executeCallCount int
}

func (p *panicPeriodicTaskExecutor) Execute(ctx context.Context, log logrus.FieldLogger, orgID uuid.UUID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.executeCallCount++
	panic("test panic")
}

func (p *panicPeriodicTaskExecutor) GetExecuteCallCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.executeCallCount
}

// Test helpers
func createTestExecutors() map[PeriodicTaskType]PeriodicTaskExecutor {
	return map[PeriodicTaskType]PeriodicTaskExecutor{
		PeriodicTaskTypeRepositoryTester:       &mockPeriodicTaskExecutor{},
		PeriodicTaskTypeResourceSync:           &mockPeriodicTaskExecutor{},
		PeriodicTaskTypeDeviceDisconnected:     &mockPeriodicTaskExecutor{},
		PeriodicTaskTypeRolloutDeviceSelection: &mockPeriodicTaskExecutor{},
		PeriodicTaskTypeDisruptionBudget:       &mockPeriodicTaskExecutor{},
		PeriodicTaskTypeEventCleanup:           &mockPeriodicTaskExecutor{},
	}
}

func createTaskReference(taskType PeriodicTaskType) PeriodicTaskReference {
	return PeriodicTaskReference{
		Type:  taskType,
		OrgID: uuid.New(),
	}
}

func TestConsumer_processTask_Success(t *testing.T) {
	tests := []struct {
		name     string
		taskType PeriodicTaskType
	}{
		{"RepositoryTester", PeriodicTaskTypeRepositoryTester},
		{"ResourceSync", PeriodicTaskTypeResourceSync},
		{"DeviceDisconnected", PeriodicTaskTypeDeviceDisconnected},
		{"RolloutDeviceSelection", PeriodicTaskTypeRolloutDeviceSelection},
		{"DisruptionBudget", PeriodicTaskTypeDisruptionBudget},
		{"EventCleanup", PeriodicTaskTypeEventCleanup},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executors := createTestExecutors()
			mockExecutor := executors[tt.taskType].(*mockPeriodicTaskExecutor)
			log := logrus.New()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			taskRef := createTaskReference(tt.taskType)

			channelManagerConfig := ChannelManagerConfig{Log: log}
			channelManager, err := NewChannelManager(channelManagerConfig)
			require.NoError(t, err)
			defer channelManager.Close()

			consumer, err := NewPeriodicTaskConsumer(PeriodicTaskConsumerConfig{
				ChannelManager: channelManager,
				Log:            log,
				Executors:      executors,
			})
			require.NoError(t, err)

			go consumer.Run(ctx)

			err = channelManager.PublishTask(ctx, taskRef)
			require.NoError(t, err)

			require.Eventually(t, func() bool {
				return mockExecutor.GetExecuteCallCount() == 1
			}, 1*time.Second, 10*time.Millisecond)

			executeCallArgs := mockExecutor.GetExecuteCallArgs()
			require.Len(t, executeCallArgs, 1)

			executedOrgID := executeCallArgs[0].orgID
			require.Equal(t, taskRef.OrgID, executedOrgID, "organization ID should be passed to executor")
		})
	}
}

func TestConsumer_processTask_MultipleTaskTypes(t *testing.T) {
	executors := createTestExecutors()
	log := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testCases := []PeriodicTaskType{
		PeriodicTaskTypeRepositoryTester,
		PeriodicTaskTypeResourceSync,
		PeriodicTaskTypeEventCleanup,
	}

	channelManagerConfig := ChannelManagerConfig{Log: log}
	channelManager, err := NewChannelManager(channelManagerConfig)
	require.NoError(t, err)
	defer channelManager.Close()

	consumer, err := NewPeriodicTaskConsumer(PeriodicTaskConsumerConfig{
		ChannelManager: channelManager,
		Log:            log,
		Executors:      executors,
	})
	require.NoError(t, err)

	go consumer.Run(ctx)

	// Execute each task type
	for _, taskType := range testCases {
		taskRef := createTaskReference(taskType)
		err := channelManager.PublishTask(ctx, taskRef)
		require.NoError(t, err)
	}

	// Verify each corresponding executor was called exactly once
	for _, taskType := range testCases {
		mockExecutor := executors[taskType].(*mockPeriodicTaskExecutor)
		require.Eventually(t, func() bool {
			return mockExecutor.GetExecuteCallCount() == 1
		}, 1*time.Second, 10*time.Millisecond)

		executeCallArgs := mockExecutor.GetExecuteCallArgs()
		require.Len(t, executeCallArgs, 1)
	}

	// Verify other executors were not called
	allTaskTypes := []PeriodicTaskType{
		PeriodicTaskTypeRepositoryTester,
		PeriodicTaskTypeResourceSync,
		PeriodicTaskTypeDeviceDisconnected,
		PeriodicTaskTypeRolloutDeviceSelection,
		PeriodicTaskTypeDisruptionBudget,
		PeriodicTaskTypeEventCleanup,
	}

	for _, taskType := range allTaskTypes {
		mockExecutor := executors[taskType].(*mockPeriodicTaskExecutor)
		found := false
		for _, testType := range testCases {
			if testType == taskType {
				found = true
				break
			}
		}
		if !found {
			require.Equal(t, 0, mockExecutor.GetExecuteCallCount(),
				"executor for %s should not be called", taskType)
		}
	}
}

func TestConsumer_processTask_MultipleTasks(t *testing.T) {
	executors := createTestExecutors()
	log := logrus.New()

	channelManagerConfig := ChannelManagerConfig{Log: log}
	channelManager, err := NewChannelManager(channelManagerConfig)
	require.NoError(t, err)
	defer channelManager.Close()

	consumer, err := NewPeriodicTaskConsumer(PeriodicTaskConsumerConfig{
		ChannelManager: channelManager,
		Log:            log,
		Executors:      executors,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go consumer.Run(ctx)

	// Publish multiple tasks
	numTasks := 10
	for i := 0; i < numTasks; i++ {
		taskRef := createTaskReference(PeriodicTaskTypeRepositoryTester)
		err := channelManager.PublishTask(ctx, taskRef)
		require.NoError(t, err)
	}

	// Wait for all tasks to be processed
	mockExecutor := executors[PeriodicTaskTypeRepositoryTester].(*mockPeriodicTaskExecutor)
	require.Eventually(t, func() bool {
		return mockExecutor.GetExecuteCallCount() == numTasks
	}, 2*time.Second, 10*time.Millisecond)

	executeCallArgs := mockExecutor.GetExecuteCallArgs()
	require.Len(t, executeCallArgs, numTasks)
}

func TestPeriodicTaskConsumer_ContextCanceled(t *testing.T) {
	executors := createTestExecutors()
	log := logrus.New()

	channelManagerConfig := ChannelManagerConfig{Log: log}
	channelManager, err := NewChannelManager(channelManagerConfig)
	require.NoError(t, err)
	defer channelManager.Close()

	consumer, err := NewPeriodicTaskConsumer(PeriodicTaskConsumerConfig{
		ChannelManager: channelManager,
		Log:            log,
		Executors:      executors,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	consumerCtx, consumerCancel := context.WithCancel(context.Background())
	defer consumerCancel()
	go consumer.Run(consumerCtx)

	taskRef := createTaskReference(PeriodicTaskTypeRepositoryTester)
	err = channelManager.PublishTask(ctx, taskRef)
	require.NoError(t, err)

	mockExecutor := executors[PeriodicTaskTypeRepositoryTester].(*mockPeriodicTaskExecutor)
	require.Eventually(t, func() bool {
		return mockExecutor.GetExecuteCallCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	consumerCancel()

	taskRef2 := createTaskReference(PeriodicTaskTypeRepositoryTester)
	err = channelManager.PublishTask(ctx, taskRef2)
	require.NoError(t, err) // Publishing should succeed

	// Wait a bit and verify no additional tasks were processed
	time.Sleep(100 * time.Millisecond)
	finalCount := mockExecutor.GetExecuteCallCount()
	require.Equal(t, 1, finalCount, "no additional tasks should be processed after stopping consumer")
}

func TestConsumer_processTask_PanicRecovery(t *testing.T) {
	panicExecutor := &panicPeriodicTaskExecutor{}
	normalExecutor := &mockPeriodicTaskExecutor{}

	executors := map[PeriodicTaskType]PeriodicTaskExecutor{
		PeriodicTaskTypeRepositoryTester: panicExecutor,
		PeriodicTaskTypeResourceSync:     normalExecutor,
	}

	log := logrus.New()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	channelManagerConfig := ChannelManagerConfig{Log: log}
	channelManager, err := NewChannelManager(channelManagerConfig)
	require.NoError(t, err)
	defer channelManager.Close()

	consumer, err := NewPeriodicTaskConsumer(PeriodicTaskConsumerConfig{
		ChannelManager: channelManager,
		Log:            log,
		Executors:      executors,
	})
	require.NoError(t, err)

	go consumer.Run(ctx)

	// Execute a task that panics
	panicTaskRef := PeriodicTaskReference{
		Type:  PeriodicTaskTypeRepositoryTester,
		OrgID: uuid.New(),
	}

	err = channelManager.PublishTask(ctx, panicTaskRef)
	require.NoError(t, err)

	// Wait for panicking task to be processed - consumer should not crash
	require.Eventually(t, func() bool {
		return panicExecutor.GetExecuteCallCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	require.Equal(t, 1, panicExecutor.GetExecuteCallCount(), "panicking executor should have been called")

	// Verify the consumer still works for a healthy task after a panic
	normalTaskRef := PeriodicTaskReference{
		Type:  PeriodicTaskTypeResourceSync,
		OrgID: uuid.New(),
	}

	err = channelManager.PublishTask(ctx, normalTaskRef)
	require.NoError(t, err)

	// Wait for normal task to be processed
	require.Eventually(t, func() bool {
		return normalExecutor.GetExecuteCallCount() == 1
	}, 1*time.Second, 10*time.Millisecond)

	require.Equal(t, 1, normalExecutor.GetExecuteCallCount(), "normal executor should have been called")
}
