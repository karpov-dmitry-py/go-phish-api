package elastic

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"phish-api/internal/validate"

	"github.com/elastic/go-elasticsearch/v6"
	"github.com/elastic/go-elasticsearch/v6/esutil"
)

type ElasticConfig struct {
	Index         string        `yaml:"index"`
	Hosts         []string      `yaml:"hosts"`
	UserName      string        `yaml:"username"`
	Password      string        `yaml:"password"`
	MaxRetries    int           `yaml:"max_retries"`
	SleepTime     time.Duration `yaml:"sleep_time"`
	FlushInterval time.Duration `yaml:"flush_interval"`
	Who           string        `yaml:"who"`
}

func (cfg ElasticConfig) IsValid() bool {
	valid := true
	part := "[elastic validation]"

	if cfg.Index == "" {
		valid = false
		log.Printf("%v index is invalid", part)
	}

	if len(cfg.Hosts) == 0 {
		valid = false
		log.Printf("%v hosts list is empty", part)
	}

	for _, host := range cfg.Hosts {
		if !validate.IsValidUrl(host) {
			valid = false
			log.Printf("%v host '%v' is invalid", part, host)
		}
	}

	if cfg.MaxRetries <= 1 {
		valid = false
		log.Printf("%v retries count is invalid", part)
	}

	if cfg.SleepTime < time.Millisecond {
		valid = false
		log.Printf("%v sleep time is invalid", part)
	}

	if cfg.FlushInterval < time.Millisecond {
		valid = false
		log.Printf("%v flush interval is invalid", part)
	}

	if cfg.Who == "" {
		valid = false
		log.Printf("%v 'who' is empty", part)
	}

	return valid
}

type BulkIndexer struct {
	es   *elasticsearch.Client
	bulk esutil.BulkIndexer
}

func (e *Elastic) NewBulkIndexer() (*BulkIndexer, error) {
	bulk, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        e.Client,
		DocumentType:  "_doc",
		NumWorkers:    2,               // default: NumCPUs
		FlushInterval: e.FlushInterval, // default: 30 secs
		// FlushBytes:    1024 * 1024,     // defaul: 5Mb
		OnError: func(ctx context.Context, err error) {
			log.Printf("elastic error: %s", err)
		},
	})
	if err != nil {
		log.Printf("elastic new bulk indexer fail, err: %s", err)
		return nil, err
	}
	return &BulkIndexer{es: e.Client, bulk: bulk}, nil
}

func (b *BulkIndexer) Close() error {
	return b.bulk.Close(context.Background())
}

func (b *BulkIndexer) BulkStats() esutil.BulkIndexerStats {
	return b.bulk.Stats()
}

type task struct {
	r  io.Reader
	sf func()
}

func (t *task) Read(p []byte) (n int, err error) {
	return t.r.Read(p)
}

func (b *BulkIndexer) Index(index string, itm interface{}, onSuccess func()) error {
	t := task{r: esutil.NewJSONReader(itm), sf: onSuccess}
	return b.bulk.Add(
		context.Background(),
		esutil.BulkIndexerItem{
			Index:  index,
			Action: "index",
			Body:   &t,
			OnSuccess: func(c context.Context, bii esutil.BulkIndexerItem, biri esutil.BulkIndexerResponseItem) {
				t := bii.Body.(*task)
				if t.sf != nil {
					t.sf()
				}
			},
			OnFailure: func(c context.Context, bii esutil.BulkIndexerItem, biri esutil.BulkIndexerResponseItem, e error) {
				log.Fatalf("elastic: %v", biri.Error)
			},
		},
	)
}

type Elastic struct {
	Client        *elasticsearch.Client
	Indexer       *BulkIndexer
	Index         string
	Who           string
	FlushInterval time.Duration
}

func NewElastic(cfg ElasticConfig) (*Elastic, error) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses:            cfg.Hosts,
		Username:             cfg.UserName,
		Password:             cfg.Password,
		EnableRetryOnTimeout: true,
		RetryOnStatus:        []int{429, 502, 503, 504},
		MaxRetries:           cfg.MaxRetries,
		RetryBackoff: func(i int) time.Duration {
			if i == cfg.MaxRetries {
				log.Fatalf(" elastic fail: max retries have been reached: %v", cfg.MaxRetries)
			}
			log.Printf("elastic - current retry: %v", i)
			return cfg.SleepTime
		},
	})
	if err != nil {
		log.Printf("elastic fail, err: %v", err)
		return nil, err
	}

	el := &Elastic{Client: client, FlushInterval: cfg.FlushInterval}

	indexer, err := el.NewBulkIndexer()
	if err != nil {
		return nil, err
	}
	el.Indexer = indexer

	el.Index = cfg.Index
	el.Who = cfg.Who

	return el, nil
}

type LogTask struct {
	When      time.Time   `json:"time"`
	Who       string      `json:"who"`
	StartTime time.Time   `json:"-"`
	Referrer  string      `json:"referrer"`
	Action    string      `json:"action"`
	Success   bool        `json:"success"`
	Duration  float64     `json:"duration"`
	URL       string      `json:"url"`
	Domain    string      `json:"domain"`
	Source    string      `json:"source"`
	Store     bool        `json:"store"`
	Desc      interface{} `json:"desc,omitempty"`
}

func (el *Elastic) Log(task *LogTask) {
	task.When = time.Now()
	task.Who = el.Who
	task.Duration = time.Since(task.StartTime).Seconds()
	if task.Desc != nil {
		task.Desc = fmt.Sprintf("%v", task.Desc)
	}

	err := el.Indexer.Index(el.Index, task, nil)
	if err != nil {
		log.Fatalf("logging to elastic fail, url: %v, error: %v", task.URL, err)
	}
}
