package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"phish-api/internal/elastic"
	mt "phish-api/internal/metrics"
	"phish-api/internal/rabbitmq"
	"phish-api/internal/validate"

	"github.com/gin-gonic/gin"
)

const (
	authHeader string = "Authorization"
)

var (
	ok_statuses = []int{200, 201, 204, 301, 302, 304}
)

type AddUrlTask struct {
	Source string `json:"source"`
	Store  bool   `json:"store,omitempty"`
	URL    string `json:"url"`
}

func (t AddUrlTask) String() string {
	return fmt.Sprintf("src: %v, store: %v, url: %v", t.Source, t.Store, t.URL)
}

func (t AddUrlTask) Validate() (bool, error) {
	var errs []string
	valid := true

	if t.Source == "" {
		valid = false
		errs = append(errs, "source is empty")
	}

	if t.URL == "" {
		valid = false
		errs = append(errs, "url is empty")

	} else {
		parsed, err := url.Parse(t.URL)
		if err != nil {
			valid = false
			errs = append(errs, fmt.Sprintf("invalid url (can't parse): %v", err))

		} else {
			scheme := parsed.Scheme
			if scheme != "http" && scheme != "https" {
				valid = false
				errs = append(errs, fmt.Sprintf("invalid scheme in url: %v", scheme))
			}
		}
	}

	return valid, errors.New(strings.Join(errs, ", "))
}

type HttpConfig struct {
	Listen     string            `yaml:"listen"`
	AuthTokens map[string]string `yaml:"auth_tokens"`
}

func (c *HttpConfig) IsValid() bool {
	var (
		valid = true
		errs  []string
	)

	cfgName := "http"
	if c.Listen == "" {
		valid = false
		errs = append(errs, fmt.Sprintf("%v empty val: 'listen'", cfgName))
	}

	if len(c.AuthTokens) == 0 {
		errs = append(errs, fmt.Sprintf("%v empty val: 'auth_tokens'", cfgName))
	}

	if len(errs) > 0 {
		log.Printf("config is invalid; errors: %v", strings.Join(errs, ", "))
	}
	return valid
}

type Server struct {
	Srv           *http.Server
	RabbitHandler *rabbitmq.RabbitHandler
	Validator     *validate.Validator
	AuthTokens    map[string]string
	AddUrlTaskCh  chan *AddUrlTask
	Elastic       *elastic.Elastic
}

func NewServer(
	cfg HttpConfig,
	rabbitHandler *rabbitmq.RabbitHandler,
	validator *validate.Validator,
	elastic *elastic.Elastic) (*Server, error) {

	// gin.SetMode(gin.ReleaseMode) // set production mode on (no debug data will be available)

	if !cfg.IsValid() {
		return nil, errors.New("http config is invalid")
	}

	router := gin.Default()
	server := &Server{
		AuthTokens:    cfg.AuthTokens,
		AddUrlTaskCh:  make(chan *AddUrlTask),
		RabbitHandler: rabbitHandler,
		Validator:     validator,
		Elastic:       elastic,

		Srv: &http.Server{
			Addr:    fmt.Sprintf(":%v", cfg.Listen),
			Handler: router,
		},
	}

	router.GET("/status", server.status)
	router.GET("/metrics", mt.PrometheusHandler())

	// api main group
	api := router.Group("/v1")
	api.Use(server.middlewareHandler)

	// url group within api
	url := api.Group("/url")
	url.POST("/add", server.addUrl)
	url.GET("/status", server.getUrlStatus)

	return server, nil
}

func (s *Server) Up() error {
	log.Printf("starting up http server on %v ...", s.Srv.Addr)
	return s.Srv.ListenAndServe()
}

func (s *Server) Down() error {
	log.Printf("shutting down http server on %v ...", s.Srv.Addr)
	return s.Srv.Shutdown(context.Background())
}

func (s *Server) middlewareHandler(c *gin.Context) {
	// check request authentication
	valid, reason := s.validateRequestAuthentication(c)
	if !valid {
		s.writeResponse(c, http.StatusUnauthorized, reason)
		return
	}
	c.Next()
}

func (s *Server) parseRequestReferrer(c *gin.Context) string {
	requestAuthHeader := c.GetHeader(authHeader)
	for k, v := range s.AuthTokens {
		if v == requestAuthHeader {
			return k
		}
	}
	return ""
}

func (s *Server) validateRequestAuthentication(c *gin.Context) (bool, string) {
	requestAuthHeader := c.GetHeader(authHeader)
	if requestAuthHeader == "" {
		return false, fmt.Sprintf("auth token '%v' is missing or empty", authHeader)

	}

	if !s.isValidAuthToken(requestAuthHeader) {
		return false, fmt.Sprintf("auth token '%v' is invalid", authHeader)
	}

	return true, ""
}

func (s *Server) writeResponse(c *gin.Context, status int, message interface{}) {
	if isOkStatus(status) {
		c.JSON(status, message)
	} else {
		c.AbortWithStatusJSON(status, gin.H{"error": message})
	}
	mt.IncVec(mt.ResponseStatuses, fmt.Sprintf("%v", status))
}

func (s *Server) isValidAuthToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, val := range s.AuthTokens {
		if strings.ToLower(strings.TrimSpace(val)) == token {
			return true
		}
	}
	return false
}

func isOkStatus(status int) bool {
	for _, val := range ok_statuses {
		if val == status {
			return true
		}
	}
	return false
}

// route handlers
func (s *Server) status(c *gin.Context) {
	s.writeResponse(c, http.StatusOK, gin.H{"status": "ok"})
}

func (s *Server) addUrl(c *gin.Context) {
	var task AddUrlTask
	var errMsg string
	errPrfx := "invalid add url task"
	action := "add url"

	log.Printf("received a new task: %v", action)
	if err := c.BindJSON(&task); err != nil {
		errMsg = fmt.Sprintf("%v: can't parse json: %v", errPrfx, err)
		s.writeResponse(c, http.StatusBadRequest, errMsg)
		return
	}

	valid, err := task.Validate()
	if !valid {
		errMsg = fmt.Sprintf("%v: %v", errPrfx, err)
		s.writeResponse(c, http.StatusBadRequest, errMsg)
		return
	}

	mustAddUrl, err := s.Validator.UrlRequiresProcessing(task.URL)
	if err != nil {
		errMsg = fmt.Sprintf("failed to check url: %v", err)
		s.writeResponse(c, http.StatusInternalServerError, errMsg)
		return
	}

	if !mustAddUrl {
		msg := fmt.Sprintf("url does not need to be added into the phishing system: %v", task.URL)
		s.writeResponse(c, http.StatusOK, msg)
		return
	}

	bytes, err := json.Marshal(task)
	if err != nil {
		errMsg = fmt.Sprintf("failed to marshal an 'add url' task to json, err: %v", err)
		s.writeResponse(c, http.StatusInternalServerError, errMsg)
		log.Fatal(errMsg)
	}

	s.RabbitHandler.Publish(task.Source, "", bytes)
	log.Printf("pushed task (%v) to dst rabbit: %v", action, task)

	// log to elastic
	log := &elastic.LogTask{
		StartTime: time.Now(),
		Action:    action,
		Referrer:  s.parseRequestReferrer(c),
		Success:   true,
		URL:       task.URL,
		Domain:    s.getDomain(task.URL),
		Source:    task.Source,
		Store:     task.Store,
	}
	go s.Elastic.Log(log)

	s.writeResponse(c, http.StatusOK, gin.H{"result": "ok"})
}

func (s *Server) getUrlStatus(c *gin.Context) {
	s.writeResponse(c, http.StatusOK, gin.H{"to do": "get url status"})
}

func (s *Server) getDomain(url string) string {
	_, domain, err := s.Validator.ParseDomain(url)
	if err != nil {
		return ""
	}
	return domain
}
