package main

// shireiActivityJava is loaded from the shared embed FS (see ios_embed.go /
// runtimeEmbed). Soft keyboard + InputConnection host; native halves live in
// androidbackend/jni.c.
func shireiActivityJavaBytes() ([]byte, error) {
	return runtimeEmbed.ReadFile("embed/ShireiActivity.java")
}
