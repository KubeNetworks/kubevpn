package util

import (
	"fmt"
	"io"
	"text/tabwriter"

	"golang.org/x/exp/constraints"
	corev1 "k8s.io/api/core/v1"
)

func PrintStatus(pod *corev1.Pod, writer io.Writer) {
	w := tabwriter.NewWriter(writer, 1, 1, 1, ' ', 0)
	defer w.Flush()
	show := func(name string, v1, v2 any) {
		_, _ = fmt.Fprintf(w, "%s\t%v\t%v\n", name, v1, v2)
	}

	show("Container", "Reason", "Message")
	for _, status := range pod.Status.ContainerStatuses {
		if status.State.Waiting != nil {
			show(status.Name, status.State.Waiting.Reason, status.State.Waiting.Message)
		}
		if status.State.Running != nil {
			show(status.Name, "ContainerRunning", "")
		}
		if status.State.Terminated != nil {
			show(status.Name, status.State.Terminated.Reason, status.State.Terminated.Message)
		}
	}
}

func max[T constraints.Ordered](a T, b T) T {
	if a > b {
		return a
	}
	return b
}
