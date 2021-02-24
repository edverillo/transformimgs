package img

type Queue struct {
	ops chan *Command
}

type OpCallback func()

func NewQueue() *Queue {
	q := &Queue{}
	q.ops = make(chan *Command)
	go q.start()
	return q
}

func (q *Queue) start() {
	for op := range q.ops {
		if op.Result == nil {
			op.Result, op.Err = op.Transformation(op.Config)
		}
		op.FinishedCond.L.Lock()
		op.Finished = true
		op.FinishedCond.L.Unlock()

		op.FinishedCond.Signal()
	}
}

func (q *Queue) AddAndWait(op *Command, callback OpCallback) {
	//Adding operation to the execution channel
	q.ops <- op

	//Waiting for operation to finish
	op.FinishedCond.L.Lock()
	for !op.Finished {
		op.FinishedCond.Wait()
	}
	op.FinishedCond.L.Unlock()

	callback()
}
