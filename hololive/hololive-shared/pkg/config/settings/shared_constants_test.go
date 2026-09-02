package settings

const (
	serviceHololiveAPI    = "hololive-api"
	serviceAlarmWorker    = "hololive-alarm-worker"
	serviceAdminDashboard = "admin-dashboard"
	serviceHoloPostgres   = "holo-postgres"

	runtimeCertsDir    = "/run/hololive-bot/certs"
	hololiveH3KeyPath  = "/run/hololive-bot/certs/hololive-h3.key"
	postgresCACertPath = "/run/hololive-bot/certs/postgres-ca.pem"

	composeProdFile       = "deploy/compose/docker-compose.prod.yml"
	composeLiveCompatFile = "deploy/compose/docker-compose.live-compat.yml"
)
