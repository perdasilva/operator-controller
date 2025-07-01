#### Catalog Metric Dashboard (WIP)

All commands should be run from hack/catalogs/metrics

### Prereqs

- Docker
- Go
- Access to the Red Hat catalogs (and you've already done a docker login to the relevant repos)

### Start metrics collector

The collector unpacks 

```terminal
$ go mod tidy && go mod vendor && go run collector/cmd/main.go 
```

This will create a cache directory with all catalog and bundle images unpacked.
There's no retry atm so you may need to ctrl-C and re-run if the image repos start to throttle you


### Run Prometheus

```terminal
$ docker run --name prometheus -d -p 9090:9090 -v $(pwd)/prometheus.yml:/etc/prometheus/prometheus.yml --add-host=host.docker.internal:host-gateway prom/prometheus
```

### Run Grafana

```terminal
 docker run --name grafana -d -p 9000:3000 \                                                                                                                                                                                                                                                              130 ↵ perdasilva@nibbler
 -v $(pwd)/grafana-config/grafana.ini:/etc/grafana/grafana.ini \
 -v $(pwd)/grafana-config/dashboards:/etc/grafana/dashboards \
  -v $(pwd)/grafana-config/provisioning/dashboards:/etc/grafana/provisioning/dashboards \
  -v $(pwd)/grafana-config/provisioning/datasources:/etc/grafana/provisioning/datasources \
 --add-host=host.docker.internal:host-gateway grafana/grafana:latest
```

You can access grafana on http://localhost:9000 username/password: olm
