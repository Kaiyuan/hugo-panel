package hugo

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const maxCommandOutput = 256 * 1024

type limitedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if b.max <= 0 {
		return n, nil
	}
	remain := b.max - b.buf.Len()
	if remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	return n, nil
}

func (b *limitedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(b.buf.String())
}

type cmdResult struct {
	Output   string
	ExitCode int
	Duration time.Duration
	Err      error
}

func runCommand(ctx context.Context, dir string, timeout time.Duration, env []string, name string, args ...string) cmdResult {
	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Env = append(os.Environ(), env...)
	out := &limitedBuffer{max: maxCommandOutput}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	res := cmdResult{Output: out.String(), Duration: time.Since(start), Err: err}
	if err == nil {
		res.ExitCode = 0
		return res
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		res.ExitCode = exitErr.ExitCode()
	} else {
		res.ExitCode = -1
	}
	if cctx.Err() == context.DeadlineExceeded {
		res.Err = fmt.Errorf("command timed out after %s", timeout)
	}
	return res
}
