package service

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Probe is the bounded worker's reading of a file. Duration comes from this
// result, not from the client's JSON.
type Probe struct {
	Duration time.Duration
	Format   string
	Width    int
	Height   int
}

type probeDocument struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Duration     string `json:"duration"`
		Frames       string `json:"nb_read_frames"`
	} `json:"streams"`
	Format struct {
		FormatName string `json:"format_name"`
		Duration   string `json:"duration"`
	} `json:"format"`
}

// ProbeFile checks the container magic, then runs ffprobe with a deadline.
// The child environment is only PATH, LC_ALL, and TZ.
func ProbeFile(ctx context.Context, ffprobe, path string, limits Limits) (Probe, error) {
	if err := magic(path); err != nil {
		return Probe{}, err
	}
	cmd := exec.CommandContext(ctx, ffprobe,
		"-v", "error",
		"-hide_banner",
		"-count_frames",
		"-select_streams", "v:0",
		"-show_entries", "stream=codec_type,width,height,nb_read_frames,avg_frame_rate,duration:format=format_name,duration",
		"-of", "json",
		path,
	)
	cmd.Env = []string{
		"PATH=" + pathEnv(),
		"LC_ALL=C.UTF-8",
		"TZ=UTC",
	}
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &limitedWriter{buf: &stdout, max: 1 << 20}
	cmd.Stderr = &limitedWriter{buf: &stderr, max: 4096}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return Probe{}, ctx.Err()
		}
		return Probe{}, errMalformed
	}
	var doc probeDocument
	if err := json.Unmarshal(stdout.Bytes(), &doc); err != nil {
		return Probe{}, errMalformed
	}
	return interpret(doc, limits)
}

func magic(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return errMalformed
	}
	defer file.Close()
	buf := make([]byte, 12)
	n, err := io.ReadFull(file, buf)
	if err != nil || n < 12 || string(buf[4:8]) != "ftyp" {
		return errMalformed
	}
	return nil
}

func interpret(doc probeDocument, limits Limits) (Probe, error) {
	if !formatAllowed(doc.Format.FormatName) || len(doc.Streams) == 0 {
		return Probe{}, errMalformed
	}
	stream := doc.Streams[0]
	if stream.CodecType != "video" || stream.Width < 1 || stream.Height < 1 {
		return Probe{}, errMalformed
	}
	if stream.Width > limits.MaxDimension || stream.Height > limits.MaxDimension {
		return Probe{}, errDimensions
	}
	formatSeconds, err := strconv.ParseFloat(doc.Format.Duration, 64)
	if err != nil || formatSeconds <= 0 {
		return Probe{}, errMalformed
	}
	seconds := formatSeconds
	if frames, rate, ok := frameRate(stream.Frames, stream.AvgFrameRate); ok {
		estimated := float64(frames) / rate
		if estimated > seconds {
			seconds = estimated
		}
	}
	if seconds > float64(limits.MaxDurationSeconds) {
		return Probe{}, errDuration
	}
	return Probe{
		Duration: time.Duration(seconds * float64(time.Second)),
		Format:   doc.Format.FormatName,
		Width:    stream.Width,
		Height:   stream.Height,
	}, nil
}

func formatAllowed(name string) bool {
	for _, part := range strings.Split(name, ",") {
		if part == "mp4" {
			return true
		}
	}
	return false
}

func frameRate(frames, rate string) (int, float64, bool) {
	count, err := strconv.Atoi(frames)
	if err != nil || count < 1 {
		return 0, 0, false
	}
	numText, denText, ok := strings.Cut(rate, "/")
	if !ok {
		return 0, 0, false
	}
	num, err := strconv.ParseFloat(numText, 64)
	if err != nil || num <= 0 {
		return 0, 0, false
	}
	den, err := strconv.ParseFloat(denText, 64)
	if err != nil || den <= 0 {
		return 0, 0, false
	}
	return count, num / den, true
}

func pathEnv() string {
	if value := os.Getenv("PATH"); value != "" {
		return value
	}
	return "/usr/bin:/bin"
}

type limitedWriter struct {
	buf *bytes.Buffer
	max int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	room := w.max - w.buf.Len()
	if room > 0 {
		if len(p) > room {
			_, _ = w.buf.Write(p[:room])
		} else {
			_, _ = w.buf.Write(p)
		}
	}
	return len(p), nil
}
