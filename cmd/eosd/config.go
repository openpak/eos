package main

// The SDK config document: GET /sdk/v1/default?platformId=<id> (and the per-product form the
// newer SDKs use, /sdk/v1/product/<productId>?...). The SDK reads it before anything else and
// retries forever until it gets one it can use (docs/protocol.md §3, §4).
//
// It is the SDK's service map: every interface takes its BaseUrl from here. This one is written
// by hand, not copied, and points every service at api.epicgames.dev, so nothing the SDK does
// afterwards leaves for a name OpenPak does not answer. Sections the SDK has no entry for fall
// back to its compiled defaults; rate limiters are left out on purpose (no limits).

import (
	"net/http"
	"strings"
)

const base = "https://api.epicgames.dev"

// services maps each EOS service name the SDK looks up to its base URL here.
var services = map[string]string{
	"AccountsEpicIdService":          "",
	"AccountsService":                "/epic/account",
	"AntiCheatService":               "/anticheat",
	"CatalogService":                 "/epic/catalog",
	"CustomInvitesService":           "/notifications",
	"DataStorageService":             "/datastorage",
	"EOSAuthService":                 "",
	"EULATrackingService":            "/epic/eulatracking",
	"EcommerceEpicIdService":         "/epic/ecom",
	"EcommerceService":               "/epic/ecommerceintegration",
	"FriendEpicIdService":            "/epic/friends",
	"FriendService":                  "/epic/friends",
	"GalleryService":                 "/epic/gallery",
	"InventoryService":               "/inventory",
	"KWSService":                     "/kws",
	"LeaderboardsService":            "/leaderboards",
	"LobbyService":                   "/lobby",
	"MatchmakingService":             "/epic/matchmaking",
	"MetricsService":                 "/datarouter",
	"ModsService":                    "/mods",
	"PartiesInternalService":         "/epic/party/internal",
	"PartiesService":                 "/epic/party",
	"PaymentEpicIdService":           "/epic/payment",
	"PostPartyService":               "/epic/postparty",
	"PresenceService":                "/epic/presence",
	"PriceEngineService":             "/epic/priceengine",
	"ProgressionSnapshotService":     "/snapshots",
	"RTCService":                     "/rtc",
	"ReceiptValidatorService":        "/receipt-validator",
	"ReportsService":                 "/player-reports",
	"SanctionsService":               "/sanctions",
	"SessionsService":                "/matchmaking",
	"StatsAchievementsService":       "/stats",
	"StatsIngestService":             "/ingestion/stats",
	"TextChatFNService":              "/epic/chat",
	"TextChatGatewayServerFNService": "/epic/chat",
	"TextChatTrustedServerFNService": "/epic/chat",
	"TitleStorageService":            "/titlestorage",
	"WeblinksService":                "/external-session-sync",
}

func sdkConfigDocument() map[string]any {
	svc := map[string]any{}
	for name, path := range services {
		svc[name] = map[string]any{"BaseUrl": base + path}
	}
	return map[string]any{
		"client": map[string]any{
			"BaseService": map[string]any{
				"HttpRetryLimit":         4,
				"HttpRetryResponseCodes": []int{429, 503, 504},
			},
			"HTTP": map[string]any{
				"HttpTimeout":           30,
				"HttpConnectionTimeout": 60,
				"HttpReceiveTimeout":    30,
				"HttpSendTimeout":       30,
				"bEnableHttp":           true,
				"MaxFlushTimeSeconds":   2.0,
			},
			"ConnectClient": map[string]any{
				"MaxProductUserIdMappingsQueryUserIds":        128,
				"MinProductUserIdMappingsUpdateTimeInSeconds": 900.0,
			},
			"LobbyClient": map[string]any{
				"LobbySocketURL": "wss://api.epicgames.dev/lobby/v1/`deploymentId/lobbies/connect",
			},
			"Messaging.Stomp": map[string]any{
				"Url":                                "wss://api.epicgames.dev/notifications/v1/`deploymentid`/connect",
				"ClientHeartbeatIntervalMs":          30000,
				"RequestedServerHeartbeatIntervalMs": 0,
			},
			"Messaging.EpicConnect": map[string]any{
				"Url":             "wss://api.epicgames.dev/epic-connect",
				"Urlv2":           "wss://api.epicgames.dev/epic-connect/v2",
				"UseV2Connection": false,
			},
			"XMPP": map[string]any{
				// No XMPP service here: the SDK's Epic-account friends chat is not part of any
				// title this serves.
				"bEnabled": false,
			},
			"SDKAnalytics": map[string]any{
				"BaseUrl":        base + "/telemetry/data/",
				"DevPhase":       2,
				"AppEnvironment": "Production",
				"UploadType":     "sdkevents",
			},
			"SessionsClient": map[string]any{"HeartbeatIntervalSecs": 30},
			"PresenceClient": map[string]any{"bSetOfflineOnLogoutEnabled": true, "bSetOfflineOnShutdownEnabled": true},
			"Overlay":        map[string]any{"ValidDomains": []string{".epicgames.dev"}},
		},
		"services":  svc,
		"watermark": 20261010,
	}
}

func (s *server) sdkConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, 405, "errors.com.epicgames.common.method_not_allowed", "method not allowed")
		return
	}
	if !strings.HasPrefix(r.URL.Path, "/sdk/v1/default") && !strings.HasPrefix(r.URL.Path, "/sdk/v1/product/") {
		s.unhandled(w, r)
		return
	}
	writeJSON(w, 200, sdkConfigDocument())
}
