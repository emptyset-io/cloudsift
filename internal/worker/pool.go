package worker

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
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
	TotalTasks        int64
	CompletedTasks    int64
	FailedTasks       int64
	CurrentWorkers    int64
	PeakWorkers       int64
	AverageExecutionMs int64
	Metrics           []TaskMetrics
	mu               sync.RWMutex
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
		tasks:      make(chan Task),
		ctx:        ctx,
		cancel:     cancel,
		metrics: &PoolMetrics{
			Metrics: make([]TaskMetrics, 0),
		},
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
		var totalMs int64
		for _, m := range p.metrics.Metrics {
			totalMs += m.ExecutionMs
		}
		metrics.AverageExecutionMs = totalMs / p.metrics.CompletedTasks
	}
	
	return metrics
}

// Submit submits a task to be executed by the worker pool
func (p *Pool) Submit(task Task) {
	atomic.AddInt64(&p.metrics.TotalTasks, 1)
	p.tasks <- task
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
			
			metrics := TaskMetrics{
				StartTime:    start,
				EndTime:     time.Now(),
				ExecutionMs: executionMs,
				ErrorOccured: err != nil,
			}
			
			p.metrics.mu.Lock()
			p.metrics.Metrics = append(p.metrics.Metrics, metrics)
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
	for _, task := range tasks {
		p.Submit(task)
	}
	p.Stop()
}
