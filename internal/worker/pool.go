package worker

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"cloudsift/internal/config"
)

// TaskMetrics tracks performance metrics for a task
type TaskMetrics struct {
	StartTime    time.Time
	EndTime      time.Time
	ExecutionMs  int64
	TaskType     string
	ErrorOccured bool
}

// PoolMetrics provides metrics about the worker pool's performance
type PoolMetrics struct {
	TotalTasks         int64
	CompletedTasks     int64
	FailedTasks        int64
	CurrentWorkers     int64
	PeakWorkers        int64
	AverageExecutionMs int64
	TotalExecutionMs   int64
	mu                 sync.RWMutex
}

// Task represents a unit of work to be executed
type Task func(ctx context.Context) error

// markerTask is a special task used for synchronization
type markerTask Task

// Pool manages a pool of workers for executing tasks concurrently
type Pool struct {
	maxWorkers    int
	tasks         chan Task
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
	metrics       *PoolMetrics
	activeWorkers int64
	stopping      int32 // Using atomic for thread-safe access
}

// NewPool creates a new worker pool with the specified number of workers
func NewPool(maxWorkers int) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		maxWorkers: maxWorkers,
		tasks:      make(chan Task, maxWorkers*2), // Buffer the channel to prevent blocking
		ctx:        ctx,
		cancel:     cancel,
		metrics:    &PoolMetrics{},
	}
}

// Start starts the worker pool
func (p *Pool) Start() {
	for i := 0; i < p.maxWorkers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
}

// Stop stops the worker pool and waits for all tasks to complete
func (p *Pool) Stop() {
	// Mark pool as stopping
	if !atomic.CompareAndSwapInt32(&p.stopping, 0, 1) {
		return // Already stopping
	}

	// Signal workers to stop accepting new tasks
	p.cancel()

	// Wait for all tasks to complete and workers to exit
	p.wg.Wait()

	// Close task channel after all workers have exited
	close(p.tasks)
}

// GetMetrics returns the current metrics for the pool
func (p *Pool) GetMetrics() PoolMetrics {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()

	// Create a new metrics struct without copying the mutex
	return PoolMetrics{
		TotalTasks:         p.metrics.TotalTasks,
		CompletedTasks:     p.metrics.CompletedTasks,
		FailedTasks:        p.metrics.FailedTasks,
		CurrentWorkers:     atomic.LoadInt64(&p.activeWorkers),
		PeakWorkers:        p.metrics.PeakWorkers,
		AverageExecutionMs: p.metrics.TotalExecutionMs / max(p.metrics.CompletedTasks, 1),
		TotalExecutionMs:   p.metrics.TotalExecutionMs,
	}
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// Submit submits a task to the pool
func (p *Pool) Submit(task Task) {
	// Don't submit if pool is stopping
	if atomic.LoadInt32(&p.stopping) == 1 {
		return
	}

	select {
	case p.tasks <- task:
		// Task submitted successfully
	case <-p.ctx.Done():
		// Pool is shutting down
	}
}

// submitMarker submits a marker task that doesn't count towards TotalTasks
func (p *Pool) submitMarker(task markerTask) {
	// Don't submit if pool is stopping
	if atomic.LoadInt32(&p.stopping) == 1 {
		return
	}

	select {
	case p.tasks <- Task(task):
	case <-p.ctx.Done():
		// Pool is shutting down
	}
}

func (p *Pool) worker() {
	defer p.wg.Done()

	atomic.AddInt64(&p.activeWorkers, 1)
	defer atomic.AddInt64(&p.activeWorkers, -1)

	// Update peak workers count if needed
	currentWorkers := atomic.LoadInt64(&p.activeWorkers)
	for {
		peak := atomic.LoadInt64(&p.metrics.PeakWorkers)
		if currentWorkers <= peak {
			break
		}
		if atomic.CompareAndSwapInt64(&p.metrics.PeakWorkers, peak, currentWorkers) {
			break
		}
	}

	for {
		select {
		case task, ok := <-p.tasks:
			if !ok {
				return
			}

			// Track task metrics
			start := time.Now()

			// Create a child context for the task that is cancelled when either:
			// 1. The pool is stopping (p.ctx is cancelled)
			// 2. The task times out (30 second timeout)
			taskCtx, cancel := context.WithTimeout(p.ctx, 30*time.Second)
			err := task(taskCtx)
			cancel()

			executionMs := time.Since(start).Milliseconds()

			p.metrics.mu.Lock()
			p.metrics.TotalExecutionMs += executionMs
			if err != nil {
				atomic.AddInt64(&p.metrics.FailedTasks, 1)
			} else {
				atomic.AddInt64(&p.metrics.CompletedTasks, 1)
			}
			p.metrics.mu.Unlock()

		case <-p.ctx.Done():
			// Finish any remaining tasks in our channel before exiting
			// but still use a timeout context for each task
			for {
				select {
				case task, ok := <-p.tasks:
					if !ok {
						return
					}
					// Create a new timeout context since pool context is already cancelled
					taskCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					if err := task(taskCtx); err != nil {
						atomic.AddInt64(&p.metrics.FailedTasks, 1)
					}
					cancel()
				default:
					return
				}
			}
		}
	}
}

// WaitForTasks waits for all currently submitted tasks to complete without stopping the pool
func (p *Pool) WaitForTasks() {
	// Create a WaitGroup to track all pending tasks
	var wg sync.WaitGroup
	wg.Add(1)

	// Submit a marker task that will only complete after all previous tasks
	p.submitMarker(markerTask(func(ctx context.Context) error {
		wg.Done()
		return nil
	}))

	// Wait for all tasks to complete
	wg.Wait()
}

// ExecuteTasks executes a slice of tasks concurrently using the worker pool
func (p *Pool) ExecuteTasks(tasks []Task) {
	// Create a WaitGroup to track all tasks
	var wg sync.WaitGroup
	wg.Add(len(tasks))

	// Update total task count
	p.metrics.mu.Lock()
	p.metrics.TotalTasks += int64(len(tasks))
	p.metrics.mu.Unlock()

	// Wrap each task to track completion
	for _, t := range tasks {
		task := t // Create new variable for closure
		wrappedTask := func(ctx context.Context) error {
			defer wg.Done()
			return task(ctx)
		}

		// Submit tasks with backpressure
		select {
		case <-p.ctx.Done():
			return // Pool is shutting down
		default:
			p.Submit(wrappedTask)
		}
	}

	// Wait for all tasks to complete
	wg.Wait()
}

var (
	// singleton instance of the pool
	sharedPool *Pool
	// mutex for safe initialization of the shared pool
	poolMutex sync.Mutex
)

// GetSharedPool returns the shared worker pool instance.
// If the pool hasn't been initialized, it will be created using the MaxWorkers from global config.
func GetSharedPool() *Pool {
	poolMutex.Lock()
	defer poolMutex.Unlock()

	if sharedPool == nil {
		sharedPool = NewPool(config.Config.MaxWorkers)
		sharedPool.Start()
	}
	return sharedPool
}

// InitSharedPool initializes the shared worker pool with the specified number of workers.
// This should be called early in the application lifecycle if you want to customize the pool size.
// If the pool is already initialized, this call will be ignored.
func InitSharedPool(maxWorkers int) error {
	poolMutex.Lock()
	defer poolMutex.Unlock()

	if sharedPool != nil {
		return nil // Pool already initialized
	}

	if maxWorkers <= 0 {
		return fmt.Errorf("maxWorkers must be greater than 0, got %d", maxWorkers)
	}

	sharedPool = NewPool(maxWorkers)
	sharedPool.Start()
	return nil
}
