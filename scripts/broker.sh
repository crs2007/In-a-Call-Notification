#!/usr/bin/env bash
# Drives the local Mosquitto broker used for development and integration tests.
# Mirror of scripts/broker.ps1 for macOS / Linux.
#
# The mosquitto client tools are not installed on the host, so subscribing and
# publishing run inside the broker container.
#
#   scripts/broker.sh up
#   scripts/broker.sh watch
#   scripts/broker.sh pub desktop-presence/test/call '{"state":"active"}'
set -euo pipefail

container=callmqtt-broker
root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

usage() {
  echo "usage: $0 up|down|status|watch|pub <topic> <payload>" >&2
  exit 2
}

[[ $# -ge 1 ]] || usage

case $1 in
  up)
    docker compose -f "$root/docker-compose.yml" up -d
    echo "Broker listening on localhost:1883"
    ;;
  down)
    docker compose -f "$root/docker-compose.yml" down
    ;;
  status)
    docker ps --filter "name=$container" --format '{{.Names}}  {{.Status}}'
    ;;
  watch)
    # -v prints "topic payload"; Ctrl-C to stop.
    docker exec -it "$container" mosquitto_sub -t '#' -v
    ;;
  pub)
    [[ $# -eq 3 ]] || { echo "pub needs a topic and a payload" >&2; exit 2; }
    docker exec "$container" mosquitto_pub -t "$2" -m "$3"
    ;;
  *)
    usage
    ;;
esac
