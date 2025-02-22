package worker

import (
	"context"
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

// Pool manages a pool of workers for executing tasks concurrently
type Pool struct {
	maxWorkers    int
	tasks         chan Task
	wg            sync.WaitGroup
	ctx           context.Context
	cancel        context.CancelFunc
	metrics       *PoolMetrics
	activeWorkers int64
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
	p.cancel()
	close(p.tasks)
	p.wg.Wait()
}

// GetMetrics returns a copy of the current pool metrics
func (p *Pool) GetMetrics() PoolMetrics {
	p.metrics.mu.RLock()
	defer p.metrics.mu.RUnlock()
	
	metrics := *p.metrics
	metrics.CurrentWorkers = atomic.LoadInt64(&p.activeWorkers)
	
	// Calculate average execution time
	if p.metrics.CompletedTasks > 0 {
		metrics.AverageExecutionMs = metrics.TotalExecutionMs / p.metrics.CompletedTasks
	}
	
	return metrics
}

// Submit submits a task to be executed by the worker pool
func (p *Pool) Submit(task Task) {
	atomic.AddInt64(&p.metrics.TotalTasks, 1)
	select {
	case p.tasks <- task:
		// Task submitted successfully
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
			err := task(p.ctx)
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
			return
		}
	}
}

// ExecuteTasks executes a slice of tasks concurrently using the worker pool
func (p *Pool) ExecuteTasks(tasks []Task) {
	p.Start()
	
	// Submit tasks with backpressure
	for _, task := range tasks {
		select {
		case <-p.ctx.Done():
			return // Pool is shutting down
		default:
			p.Submit(task)
		}
	}
	
	p.Stop()
}

var (
	// singleton instance of the pool
	sharedPool *Pool
	// mutex for safe initialization of the shared pool
	initOnce sync.Once
)

// GetSharedPool returns the shared worker pool instance.
// If the pool hasn't been initialized, it will be created using the MaxWorkers from global config.
func GetSharedPool() *Pool {
	initOnce.Do(func() {
		sharedPool = NewPool(config.Config.MaxWorkers)
		sharedPool.Start()
	})
	return sharedPool
}

// InitSharedPool initializes the shared worker pool with the specified number of workers.
// This should be called early in the application lifecycle if you want to customize the pool size.
// If the pool is already initialized, this call will be ignored.
func InitSharedPool(maxWorkers int) {
	initOnce.Do(func() {
		sharedPool = NewPool(maxWorkers)
		sharedPool.Start()
	})
}
