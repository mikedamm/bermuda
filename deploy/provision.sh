#!/usr/bin/env bash
# Create the Magic Containers app, or refresh its environment if it exists.
# CI only rolls new image tags (.github/workflows/deploy.yml); everything else
# about the app lives here. Secrets come from the environment, never argv:
#
#   BUNNYNET_API_KEY  bunny.net account API key
#   WV_API_TOKEN      internal API token (key `web` of the gateway-internal secret)
#   WV_API_BASE       internal API base URL, ending in /internal/v1
#   IMAGE_TAG         image tag to start from on first create (a commit SHA CI pushed)
#
# Optional: APP_NAME (wingview-seo), REGION (SIL, next to the API in Fremont),
# SITE_URL (https://www.wingview.com), REGISTRY_ID (Bunny registry holding
# GHCR credentials for mikedamm), IMAGE (mikedamm/bermuda).
#
# Prints BUNNY_APP_ID and BUNNY_HOST for the repository variables.
set -euo pipefail

: "${BUNNYNET_API_KEY:?}" "${WV_API_TOKEN:?}" "${WV_API_BASE:?}"
APP_NAME=${APP_NAME:-wingview-seo}
REGION=${REGION:-SIL}
SITE_URL=${SITE_URL:-https://www.wingview.com}
REGISTRY_ID=${REGISTRY_ID:-8623}
IMAGE=${IMAGE:-mikedamm/bermuda}
API=https://api.bunny.net/mc

# Secrets never go on a command line (argv is readable by every local user):
# the API key reaches curl through a pipe (-K), request bodies through stdin,
# and jq reads the token from its environment.
call() {
	local method=$1 path=$2
	shift 2
	curl -sS --fail-with-body -K <(printf 'header = "AccessKey: %s"\n' "$BUNNYNET_API_KEY") -X "$method" \
		-H 'Content-Type: application/json' -H 'Accept: application/json' "$API$path" "$@"
}
# send METHOD PATH JSON: the body goes over stdin
send() { printf '%s' "$3" | call "$1" "$2" --data-binary @-; }

export WV_API_TOKEN
env_obj=$(jq -n --arg base "$WV_API_BASE" --arg site "$SITE_URL" '{
	WV_API_BASE: $base, WV_API_TOKEN: $ENV.WV_API_TOKEN, SITE_URL: $site,
	APP_URL: "https://app.wingview.com", DATA_DIR: "/data", GOMAXPROCS: "2", LOG_LEVEL: "info"
}')

id=$(call GET /apps | jq -r --arg n "$APP_NAME" '.items[] | select(.name == $n) | .id' | head -n1)

if [[ -z $id ]]; then
	: "${IMAGE_TAG:?IMAGE_TAG is required to create the app}"
	# the environment (with the token) is jq's stdin, not an argument
	body=$(printf '%s' "$env_obj" | jq --arg name "$APP_NAME" --arg region "$REGION" --arg reg "$REGISTRY_ID" \
		--arg ns "${IMAGE%%/*}" --arg img "${IMAGE#*/}" --arg tag "$IMAGE_TAG" '. as $env | {
		name: $name,
		runtimeType: "shared",
		terminationGracePeriodSeconds: 30,
		autoScaling: {min: 1, max: 1},
		regionSettings: {allowedRegionIds: [$region], requiredRegionIds: [$region], maxAllowedRegions: 1},
		volumes: [{name: "index", size: 1}],
		containerTemplates: [{
			name: "web",
			imageRegistryId: $reg, imageNamespace: $ns, imageName: $img, imageTag: $tag,
			imagePullPolicy: "ifNotPresent",
			environmentVariables: ($env | to_entries | map({name: .key, value: .value})),
			endpoints: [{displayName: "web", cdn: {isSslEnabled: false, portMappings: [{containerPort: 8080}]}}],
			volumeMounts: [{name: "index", mountPath: "/data"}],
			probes: {
				startup:   {httpGet: {request: {path: "/healthz", portNumber: 8080}}, initialDelaySeconds: 2, periodSeconds: 3, failureThreshold: 20},
				readiness: {httpGet: {request: {path: "/healthz", portNumber: 8080}}, periodSeconds: 10, failureThreshold: 3},
				liveness:  {httpGet: {request: {path: "/healthz", portNumber: 8080}}, periodSeconds: 20, failureThreshold: 3}
			}
		}]
	}')
	id=$(send POST /apps "$body" | jq -r .id)
	echo "created app $id" >&2
	call POST "/apps/$id/deploy" >/dev/null
	echo "deploy requested" >&2
else
	app=$(call GET "/apps/$id")
	cid=$(printf '%s' "$app" | jq -r '.containerTemplates[] | select(.name == "web") | .id')
	# The env endpoint replaces everything, and Bunny adds variables of its own
	# (BUNNY_DATABASE_* for an attached database). Keep every variable this
	# script does not own, drop retired ones, then apply ours. Both documents
	# travel over stdin: they hold secrets.
	merged=$( { printf '%s' "$app" | jq -c '[.containerTemplates[] | select(.name == "web") | .environmentVariables[]? | {(.name): .value}] | add // {}'; printf '\n%s' "$env_obj"; } |
		jq -sc '(.[0] | del(.MAPBOX_TOKEN)) + .[1]')
	send PUT "/apps/$id/containers/$cid/env" "$merged" >/dev/null
	echo "updated environment of app $id (kept: $(printf '%s' "$merged" | jq -r --argjson ours "$(printf '%s' "$env_obj" | jq -c 'keys')" 'keys - $ours | join(", ")'))" >&2
fi

# The endpoint's pull zone (development only) must keep nothing the container
# renders: every response is no-store except hashed static files. Follow the
# origin's Cache-Control, pass query strings through, never serve a stale
# copy, and purge whatever an earlier configuration stored.
pz=$(call GET "/apps/$id/endpoints" | jq -r '[.items[]? // .[]? | select(.type == "cdn") | .pullZoneId][0] // empty')
if [[ -n $pz ]]; then
	printf '%s' '{"IgnoreQueryStrings":false,"EnableSmartCache":false,"UseStaleWhileUpdating":false,"UseStaleWhileOffline":false,"CacheControlMaxAgeOverride":-1,"CacheControlPublicMaxAgeOverride":-1}' |
		curl -sS --fail-with-body -K <(printf 'header = "AccessKey: %s"\n' "$BUNNYNET_API_KEY") \
			-H 'Content-Type: application/json' -X POST --data-binary @- "https://api.bunny.net/pullzone/$pz" >/dev/null
	curl -sS --fail-with-body -K <(printf 'header = "AccessKey: %s"\n' "$BUNNYNET_API_KEY") \
		-X POST "https://api.bunny.net/pullzone/$pz/purgeCache" >/dev/null
	echo "pull zone $pz: origin cache headers, no stale serving, purged" >&2
fi

host=$(call GET "/apps/$id" | jq -r '.displayEndpoint.address // empty')
echo "BUNNY_APP_ID=$id"
echo "BUNNY_HOST=$host"
