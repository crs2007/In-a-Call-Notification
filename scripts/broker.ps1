<#
.SYNOPSIS
    Drives the local Mosquitto broker used for development and integration tests.
.DESCRIPTION
    The mosquitto client tools are not installed on the host, so subscribing and
    publishing run inside the broker container.
.EXAMPLE
    scripts/broker.ps1 up
    scripts/broker.ps1 watch
    scripts/broker.ps1 pub desktop-presence/test/call '{"state":"active"}'
#>
param(
    [Parameter(Mandatory, Position = 0)]
    [ValidateSet('up', 'down', 'watch', 'pub', 'status')]
    [string]$Command,

    [Parameter(Position = 1)] [string]$Topic,
    [Parameter(Position = 2)] [string]$Payload
)

$ErrorActionPreference = 'Stop'
$container = 'callmqtt-broker'
$root = Split-Path $PSScriptRoot -Parent

switch ($Command) {
    'up' {
        docker compose -f "$root/docker-compose.yml" up -d
        Write-Host "Broker listening on localhost:1883"
    }
    'down' {
        docker compose -f "$root/docker-compose.yml" down
    }
    'status' {
        docker ps --filter "name=$container" --format '{{.Names}}  {{.Status}}'
    }
    'watch' {
        # -v prints "topic payload"; Ctrl-C to stop.
        docker exec -it $container mosquitto_sub -t '#' -v
    }
    'pub' {
        if (-not $Topic) { throw "pub needs a topic and a payload" }
        docker exec $container mosquitto_pub -t $Topic -m $Payload
    }
}
