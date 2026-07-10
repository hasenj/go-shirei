package wl

import "sync"

var userDataMap = new(sync.Map)

func SetUserData[T any](key Proxy, value *T) {
	//println("set", key.Id())
	userDataMap.Store(key.Id(), *value)

}
func GetUserData[T any](key Proxy) (found *T, exists bool) {
	eface, is := userDataMap.Load(key.Id())
	if !is {
		return
	}
	found, exists = eface.(*T)
	return
}
func DeleteUserData(key Proxy) {
	//println("clr", key.Id())
	userDataMap.Delete(key.Id())
}
