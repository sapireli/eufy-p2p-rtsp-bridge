#!/usr/bin/env python3
"""Compile the actual metadata parser portably; retain no media payloads."""
import pathlib, subprocess, tempfile
src=pathlib.Path(__file__).with_name("udp-headers.c").read_text()
helpers=src[src.index("static uint16_t be16"):src.index("static struct sock_filter*")]
parser=src[src.index("static int parse("):src.index("static size_t fixture(")]
test=r"""
static void packet(uint8_t*p,uint32_t peer){
 memset(p,0,256);p[0]=0x45;p[9]=17;memcpy(p+12,&peer,4);
 put(p+20,0,2);p[22]=0x40;p[23]=0; p[2]=0;p[3]=100;p[24]=0;p[25]=80;
 uint8_t*b=p+28;b[0]=0xf1;b[1]=0xd0;b[2]=0;b[3]=68;b[5]=1;b[7]=1;
 memcpy(b+8,"XZYH",4);put(b+12,1300,2);put(b+14,48,4);b[20]=2;b[21]=94;
 uint8_t*v=b+24;put(v,26,4);v[4]=47;put(v+6,9,2);put(v+10,1280,2);put(v+12,720,2);put(v+14,0x12345678,4);
}
int main(void){uint8_t p[256],r[56],expected[56];uint32_t peer;inet_pton(AF_INET,"192.0.2.1",&peer);
 packet(p,peer);if(!parse(p,100,0,peer,r))return 1;video_metadata(p,100,r,r);
 if(!r[55]||le32(r+40)!=26||le32(r+44)!=0x12345678||le16(r+48)!=9||r[54]!=47)return 2;
 memcpy(expected,r,56);memset(p+74,0xaf,26);parse(p,100,0,peer,r);video_metadata(p,100,r,r);
 if(memcmp(r,expected,56))return 3; /* encrypted/coded body cannot affect metadata */
 for(unsigned n=52;n<74;n++){parse(p,n,0,peer,r);video_metadata(p,n,r,r);if(r[55])return 4;}
 packet(p,peer);put(p+52,49,4);parse(p,100,0,peer,r);video_metadata(p,100,r,r);if(r[55])return 5;
 packet(p,peer);p[36]='Q';parse(p,100,0,peer,r);video_metadata(p,100,r,r);if(r[55])return 6;
 packet(p,peer);p[33]=0;parse(p,100,0,peer,r);video_metadata(p,100,r,r);if(r[55])return 7;
 puts("video metadata: SDK fields/noncanonical flags/body privacy/truncation/length/continuation/type passed");return 0;}
"""
with tempfile.TemporaryDirectory() as d:
 p=pathlib.Path(d)/"test.c";p.write_text("#include <arpa/inet.h>\n#include <stdint.h>\n#include <stdio.h>\n#include <string.h>\n"+helpers+parser+test)
 subprocess.run(["cc","-std=c11","-Wall","-Wextra","-Werror",str(p),"-o",d+"/test"],check=True)
 subprocess.run([d+"/test"],check=True)
