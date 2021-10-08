# go-phish-api
go http api to handle phishing resources requests (auth check, prometheus metrics, pushing to rabbit, logging to elasticsearch)

### Actions ###

1. [POST] `/v1/url/add` - add url to validation and further processing (auth required)
1. [GET] `/v1/url/status` - get url current state (auth required)
3. [GET] `/status` - service health check (no auth required)
4. [GET] `/metrics/` - service prometheus metrics (no auth required)

