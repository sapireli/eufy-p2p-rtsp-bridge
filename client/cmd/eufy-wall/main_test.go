package main

import "testing"

func TestParseAvahi(t *testing.T) {
	wrong := "=;eth0;IPv4;Other;_http._tcp;local;other.local;192.168.1.3;3000;\"rtsp=8554\"\n"
	good := "=;eth0;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;192.168.1.8;3000;\"rtsp=8554\"\n"
	loopback := "=;lo;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;127.0.0.1;3000;\"rtsp=8554\"\n"
	base, err := parseAvahi(wrong + loopback + good)
	if err != nil || base != "rtsp://192.168.1.8:8554" {
		t.Fatalf("got %q, %v", base, err)
	}
	if _, err := parseAvahi(wrong); err == nil {
		t.Fatal("accepted unrelated service")
	}
	if _, err := parseAvahi(loopback); err == nil {
		t.Fatal("accepted loopback address")
	}
	base, err = parseAvahi("=;eth0;IPv4;Eufy Wall;_eufy-wall._tcp;local;bridge.local;192.168.1.8;3000;\"rtsp=8565\"\n")
	if err != nil || base != "rtsp://192.168.1.8:8565" {
		t.Fatalf("custom RTSP port: got %q, %v", base, err)
	}
}
