package worker

import (
	"context"
	"sync"
)

// Task represents a unit of work to be executed
type Task func(ctx context.Context) error

// Pool manages a pool of workers for executing tasks concurrently
type Pool struct {
	maxWorkers int
	tasks      chan Task
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
}

// NewPool creates a new worker pool with the specified number of workers
func NewPool(maxWorkers int) *Pool {
	ctx, cancel := context.WithCancel(context.Background())
	return &Pool{
		maxWorkers: maxWorkers,
		tasks:      make(chan Task),
		ctx:        ctx,
		cancel:     cancel,
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

// Submit submits a task to be executed by the worker pool
func (p *Pool) Submit(task Task) {
	p.tasks <- task
}

func (p *Pool) worker() {
	defer p.wg.Done()

	for {
		select {
		case task, ok := <-p.tasks:
			if !ok {
				return
			}
			// Execute the task
			if err := task(p.ctx); err != nil {
				// Error handling is done by the task itself
				continue
			}
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
