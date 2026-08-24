// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package scheduler

import "fmt"

type jobHeap []*Job

func (h *jobHeap) Len() int { return len(*h) }

func (h *jobHeap) Less(i, j int) bool {
	jobs := *h

	if !jobs[i].NextRunAt.Equal(jobs[j].NextRunAt) {
		return jobs[i].NextRunAt.Before(jobs[j].NextRunAt)
	}

	return jobs[i].Priority > jobs[j].Priority
}

func (h *jobHeap) Swap(i, j int) {
	jobs := *h

	jobs[i], jobs[j] = jobs[j], jobs[i]
	jobs[i].index = i
	jobs[j].index = j
}

func (h *jobHeap) Push(x any) {
	n := len(*h)
	job, ok := x.(*Job)

	if !ok {
		panic(fmt.Sprintf("jobHeap.Push got %T, want *Job", x))
	}

	if job == nil {
		panic("jobHeap.Push got nil *Job")
	}

	job.index = n
	*h = append(*h, job)
}

func (h *jobHeap) Pop() any {
	old := *h
	n := len(old)
	job := old[n-1]

	job.index = -1
	*h = old[0 : n-1]

	return job
}
