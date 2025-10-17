package converter

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type ProgressFunc func(pct int)

type Mode string

const (
	ModeCBR Mode = "CBR"
	ModeVBR Mode = "VBR"
)

type Config struct {
	MinTimeout time.Duration
	MaxTimeout time.Duration
	Mode       Mode
	CBRBitrate string
	VBRQ       int
	Threads    int
}

type Converter struct {
	cfg Config
	sem chan struct{}
}

func New(cfg Config, maxConcurrent int) *Converter {
	return &Converter{cfg: cfg, sem: make(chan struct{}, maxConcurrent)}
}

func (c *Converter) withPermit(fn func() error) error {
	c.sem <- struct{}{}
	defer func() { <-c.sem }()
	return fn()
}

func (c *Converter) Convert(ctx context.Context, inputPath, outputPath string, quality string, start, end string, durationSeconds int, onProgress ProgressFunc) error {
	return c.withPermit(func() error {
		timeout := c.cfg.MaxTimeout
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		args := []string{"-y"}
		if start != "" {
			args = append(args, "-ss", start)
		}
		if end != "" {
			args = append(args, "-to", end)
		}
		args = append(args, "-i", inputPath, "-vn", "-acodec", "libmp3lame")
		if c.cfg.Mode == ModeCBR {
			// quality is expected like 128/192/320; append 'k'
			br := c.cfg.CBRBitrate
			if quality != "" {
				br = quality + "k"
			}
			args = append(args, "-b:a", br)
		} else {
			q := fmt.Sprintf("%d", c.cfg.VBRQ)
			args = append(args, "-q:a", q)
		}
		if c.cfg.Threads > 0 {
			args = append(args, "-threads", fmt.Sprintf("%d", c.cfg.Threads))
		}
		args = append(args, "-progress", "pipe:1", "-nostats", "-loglevel", "error", outputPath)

		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return err
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { io.Copy(io.Discard, stderr) }()
		scanner := bufio.NewScanner(stdout)
		var lastPct int
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "out_time_ms=") {
				v := strings.TrimPrefix(line, "out_time_ms=")
				ms, _ := strconv.ParseFloat(v, 64)
				if durationSeconds > 0 {
					pct := int((ms / 1000000.0) / float64(durationSeconds) * 100.0)
					if pct < 0 {
						pct = 0
					}
					if pct > 100 {
						pct = 100
					}
					if pct != lastPct {
						lastPct = pct
						onProgress(pct)
					}
				}
			}
		}
		return cmd.Wait()
	})
}
