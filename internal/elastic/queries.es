PUT phish-api-logs
{
    "settings": {
        "index": {
            "number_of_shards": 2,
            "number_of_replicas": 0
        }
    },
    "mappings": {
        "properties": {
            "time": {
                "type": "date"
            },
            "who": {
                "type": "keyword"
            },
            "referrer": {
                "type": "keyword"
            },
            "action": {
                "type": "keyword"
            },
            "url": {
                "type": "keyword"
            },
            "domain": {
                "type": "keyword"
            },
            "source": {
                "type": "keyword"
            },
            "store": {
                "type": "boolean"
            },
            "success": {
                "type": "boolean"
            },
            "duration": {
                "type": "float"
            },
            "desc": {
                "type": "keyword"
            }
        }
    }
}


GET phish-api-logs/_search?size=2
{
  "query": {
    "match_all": {}
  },
  "sort": {
    "time": "desc"
  }
}