package cache

type DataCache interface {
	KeyValueCache
	SetCache
	HashCache
}

type Client interface {
	DataCache
	ScriptCache
	ConnectionManager
	LowLevelCache
}
