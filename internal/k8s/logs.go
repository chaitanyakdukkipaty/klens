package k8s

import (
	"bufio"
	"context"
	"fmt"
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

// LogGroup is a named set of pods to stream together (e.g. one Deployment).
type LogGroup struct {
	Name string
	Pods []string
}

// LogLine is a single line from a pod log stream.
type LogLine struct {
	Pod       string
	Container string
	Group     string // LogGroup.Name this pod belongs to; "" for ungrouped streams
	Text      string
	ColorIdx  int // index into styles.LogPrefixColors
	IsSystem  bool
}

// LogLineMsg carries a batch of new log lines.
type LogLineMsg struct {
	Lines []LogLine
}

// LogStreamer manages concurrent log streams from multiple pods.
type LogStreamer struct {
	cs        *kubernetes.Clientset
	namespace string
	lineCh    chan LogLine
	ctx       context.Context
	cancel    context.CancelFunc
}

func NewLogStreamer(cs *kubernetes.Clientset, namespace string) *LogStreamer {
	ctx, cancel := context.WithCancel(context.Background())
	return &LogStreamer{
		cs:        cs,
		namespace: namespace,
		lineCh:    make(chan LogLine, 512),
		ctx:       ctx,
		cancel:    cancel,
	}
}

// Start begins streaming from the given pod names (all containers merged).
func (s *LogStreamer) Start(pods []string) {
	for i, pod := range pods {
		go s.streamPodInGroup(pod, "", i%8)
	}
}

// StartGrouped begins streaming with each LogGroup tracked separately.
// Lines carry the group name so the viewer can split them into panes.
func (s *LogStreamer) StartGrouped(groups []LogGroup) {
	colorIdx := 0
	for _, g := range groups {
		for _, pod := range g.Pods {
			go s.streamPodInGroup(pod, g.Name, colorIdx%8)
			colorIdx++
		}
	}
}

// Stop cancels all active streams.
func (s *LogStreamer) Stop() { s.cancel() }

// ReadCmd returns a tea.Cmd that blocks until a batch of log lines is ready.
func (s *LogStreamer) ReadCmd() tea.Cmd {
	return func() tea.Msg {
		batch := make([]LogLine, 0, 32)
		// Block on first line
		select {
		case line, ok := <-s.lineCh:
			if !ok {
				return nil
			}
			batch = append(batch, line)
		case <-s.ctx.Done():
			return nil
		}
		// Drain available lines (up to 64 more) without blocking
		for i := 0; i < 64; i++ {
			select {
			case line := <-s.lineCh:
				batch = append(batch, line)
			default:
				goto done
			}
		}
	done:
		return LogLineMsg{Lines: batch}
	}
}

func (s *LogStreamer) streamPodInGroup(podName, group string, colorIdx int) {
	tailLines := int64(200)
	opts := &corev1.PodLogOptions{
		Follow:    true,
		TailLines: &tailLines,
	}

	backoff := time.Second
	maxBackoff := 30 * time.Second

	for attempt := 0; attempt < 10; attempt++ {
		if s.ctx.Err() != nil {
			return
		}

		req := s.cs.CoreV1().Pods(s.namespace).GetLogs(podName, opts)
		stream, err := req.Stream(s.ctx)
		if err != nil {
			s.sendSystem(podName, group, colorIdx, fmt.Sprintf("[%s] stream error: %v (retry %d)", podName, err, attempt+1))
			select {
			case <-time.After(backoff):
			case <-s.ctx.Done():
				return
			}
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		backoff = time.Second // reset on successful connect
		s.sendSystem(podName, group, colorIdx, fmt.Sprintf("[%s] connected", podName))

		scanner := bufio.NewScanner(stream)
		scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
		for scanner.Scan() {
			if s.ctx.Err() != nil {
				stream.Close()
				return
			}
			select {
			case s.lineCh <- LogLine{Pod: podName, Group: group, Text: scanner.Text(), ColorIdx: colorIdx}:
			case <-s.ctx.Done():
				stream.Close()
				return
			}
		}
		stream.Close()

		if s.ctx.Err() != nil {
			return
		}
		s.sendSystem(podName, group, colorIdx, fmt.Sprintf("[%s] stream ended, reconnecting…", podName))
		select {
		case <-time.After(backoff):
		case <-s.ctx.Done():
			return
		}
		backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
	}
	s.sendSystem(podName, group, colorIdx, fmt.Sprintf("[%s] max retries reached", podName))
}

func (s *LogStreamer) sendSystem(pod, group string, colorIdx int, msg string) {
	select {
	case s.lineCh <- LogLine{Pod: pod, Group: group, ColorIdx: colorIdx, Text: msg, IsSystem: true}:
	default:
	}
}
