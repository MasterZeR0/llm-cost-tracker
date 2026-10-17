package parsers

import "sync"

var (
	registryLock sync.RWMutex
	registry     = make(map[string]Parser)
)

func Register(p Parser) {
	registryLock.Lock()
	defer registryLock.Unlock()
	registry[p.Name()] = p
}

func Get(name string) (Parser, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()
	p, ok := registry[name]
	return p, ok
}

func GetParser(name string) (Parser, bool) {
	return Get(name)
}

func All() []Parser {
	registryLock.RLock()
	defer registryLock.RUnlock()
	list := make([]Parser, 0, len(registry))
	for _, p := range registry {
		list = append(list, p)
	}
	return list
}
