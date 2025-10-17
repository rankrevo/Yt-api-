package main

import (
	bytes "bytes"
	context "context"
	encodingjson "encoding/json"
	flag "flag"
	fmt "fmt"
	http "net/http"
	strings "strings"
	sync "sync"
	time "time"
)

type PrepareResp struct {
	ConversionID string `json:"conversion_id"`
	Status       string `json:"status"`
	Metadata     struct {
		Title     string `json:"title"`
		Duration  int    `json:"duration"`
		Thumbnail string `json:"thumbnail"`
	} `json:"metadata"`
	Message string `json:"message"`
}

type ConvertResp struct {
	ConversionID  string `json:"conversion_id"`
	Status        string `json:"status"`
	QueuePosition int    `json:"queue_position"`
	Message       string `json:"message"`
}

type StatusResp struct {
	ConversionID       string `json:"conversion_id"`
	Status             string `json:"status"`
	DownloadProgress   int    `json:"download_progress"`
	ConversionProgress int    `json:"conversion_progress"`
	DownloadURL        string `json:"download_url"`
	QueuePosition      int    `json:"queue_position"`
	Error              string `json:"error"`
}

type JobResult struct {
	URL           string
	ID            string
	OK            bool
	Err           string
	MetaMs        int64
	QueueWaitMs   int64
	DownloadMs    int64
	ConvertMs     int64
	TotalMs       int64
	PrepareStart  time.Time
	PrepareEnd    time.Time
	ConvertReqEnd time.Time
	DownloadStart time.Time
	DownloadEnd   time.Time
	ConvertStart  time.Time
	Completed     time.Time
}

func main() {
	base := flag.String("base", "http://127.0.0.1:8080", "API base URL")
	urlIn := flag.String("url", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "YouTube URL to test")
	n := flag.Int("n", 20, "number of concurrent requests")
	quality := flag.String("q", "128", "MP3 quality (128/192/256/320)")
	perIPDelay := flag.Duration("delay", 0, "stagger start delay between jobs (to avoid per-IP limits)")
	flag.Parse()

	client := &http.Client{Timeout: 30 * time.Second}

	urls := make([]string, *n)
	for i := 0; i < *n; i++ {
		// Make URLs unique for dedup-bypass (YouTube ignores unknown query params)
		sep := "&"
		if !strings.Contains(*urlIn, "?") {
			sep = "?"
		}
		urls[i] = fmt.Sprintf("%s%cutm=%d", *urlIn, sep[0], i)
	}

	results := make([]JobResult, *n)
	var wg sync.WaitGroup
	wg.Add(*n)

	for i := 0; i < *n; i++ {
		i := i
		go func() {
			defer wg.Done()
			if *perIPDelay > 0 && i > 0 {
				time.Sleep(time.Duration(i) * *perIPDelay)
			}
			res := runOne(client, *base, urls[i], *quality)
			results[i] = res
		}()
	}

	wg.Wait()

	// Print per-job summary
	fmt.Println("\nPer-job summary:")
	for i, r := range results {
		status := "OK"
		if !r.OK {
			status = "FAIL"
		}
		fmt.Printf("%2d) %s id=%s status=%s meta=%dms queue_wait=%dms download=%dms convert=%dms total=%dms\n",
			i+1, r.URL, r.ID, status, r.MetaMs, r.QueueWaitMs, r.DownloadMs, r.ConvertMs, r.TotalMs)
		if r.Err != "" {
			fmt.Printf("    error: %s\n", r.Err)
		}
	}

	// Aggregate stats (completed only)
	var c int
	var metaSum, queueSum, dlSum, cvSum, totSum int64
	for _, r := range results {
		if !r.OK {
			continue
		}
		c++
		metaSum += r.MetaMs
		queueSum += r.QueueWaitMs
		dlSum += r.DownloadMs
		cvSum += r.ConvertMs
		totSum += r.TotalMs
	}
	if c > 0 {
		fmt.Printf("\nAverages over %d completed:\n", c)
		fmt.Printf("meta=%.0fms queue_wait=%.0fms download=%.0fms convert=%.0fms total=%.0fms\n",
			float64(metaSum)/float64(c), float64(queueSum)/float64(c), float64(dlSum)/float64(c), float64(cvSum)/float64(c), float64(totSum)/float64(c))
	}
}

func runOne(client *http.Client, base, videoURL, quality string) JobResult {
	res := JobResult{URL: videoURL}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	// 1) prepare
	prepBody := map[string]string{"url": videoURL}
	pb, _ := encodingjson.Marshal(prepBody)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/prepare", bytes.NewReader(pb))
	req.Header.Set("Content-Type", "application/json")
	res.PrepareStart = time.Now()
	phttp, err := client.Do(req)
	res.PrepareEnd = time.Now()
	if err != nil {
		res.Err = "prepare: " + err.Error()
		return res
	}
	defer phttp.Body.Close()
	var pr PrepareResp
	_ = encodingjson.NewDecoder(phttp.Body).Decode(&pr)
	res.ID = pr.ConversionID
	res.MetaMs = res.PrepareEnd.Sub(res.PrepareStart).Milliseconds()
	if res.ID == "" {
		res.Err = "prepare: empty id"
		return res
	}

	// 2) convert (async 202)
	convBody := map[string]any{"conversion_id": res.ID, "quality": quality}
	cb, _ := encodingjson.Marshal(convBody)
	creq, _ := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/convert", bytes.NewReader(cb))
	creq.Header.Set("Content-Type", "application/json")
	_, _ = client.Do(creq) // ignore body; status 202 expected
	res.ConvertReqEnd = time.Now()

	// 3) poll status
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	var sawDownloading, sawDownloaded, sawConverting bool
	for {
		select {
		case <-ctx.Done():
			res.Err = "timeout"
			return res
		case <-poll.C:
			st, e := fetchStatus(client, base, res.ID)
			if e != nil {
				continue
			}
			if st.Status == "failed" {
				res.Err = st.Error
				return res
			}
			if st.Status == "downloading" && !sawDownloading {
				sawDownloading = true
				res.DownloadStart = time.Now()
			}
			if st.Status == "downloaded" && !sawDownloaded {
				sawDownloaded = true
				res.DownloadEnd = time.Now()
			}
			if st.Status == "converting" && !sawConverting {
				sawConverting = true
				res.ConvertStart = time.Now()
				// If we missed downloaded state, set it to now if dl progress is 100
				if !sawDownloaded && st.DownloadProgress == 100 {
					res.DownloadEnd = res.ConvertStart
				}
			}
			if st.Status == "completed" {
				res.Completed = time.Now()
				res.OK = true
				// compute durations
				res.TotalMs = res.Completed.Sub(res.PrepareEnd).Milliseconds()
				if !res.DownloadStart.IsZero() && !res.DownloadEnd.IsZero() {
					res.DownloadMs = res.DownloadEnd.Sub(res.PrepareEnd).Milliseconds()
				}
				if !res.ConvertStart.IsZero() {
					res.QueueWaitMs = res.ConvertStart.Sub(res.ConvertReqEnd).Milliseconds()
					res.ConvertMs = res.Completed.Sub(res.ConvertStart).Milliseconds()
				}
				return res
			}
		}
	}
}

func fetchStatus(client *http.Client, base, id string) (StatusResp, error) {
	var st StatusResp
	req, _ := http.NewRequest(http.MethodGet, strings.TrimRight(base, "/")+"/status/"+id, nil)
	resp, err := client.Do(req)
	if err != nil {
		return st, err
	}
	defer resp.Body.Close()
	_ = encodingjson.NewDecoder(resp.Body).Decode(&st)
	return st, nil
}
