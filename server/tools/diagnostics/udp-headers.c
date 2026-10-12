#define _GNU_SOURCE
#include <arpa/inet.h>
#include <errno.h>
#include <linux/filter.h>
#include <linux/if_ether.h>
#include <linux/if_packet.h>
#include <net/if.h>
#include <poll.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/resource.h>
#include <sys/socket.h>
#include <time.h>
#include <unistd.h>

static uint64_t ns(clockid_t c) { struct timespec t; clock_gettime(c,&t); return (uint64_t)t.tv_sec*1000000000+t.tv_nsec; }
static uint16_t be16(const uint8_t*p){return (p[0]<<8)|p[1];}
static uint16_t le16(const uint8_t*p){return p[0]|(p[1]<<8);}
static uint32_t le32(const uint8_t*p){return p[0]|((uint32_t)p[1]<<8)|((uint32_t)p[2]<<16)|((uint32_t)p[3]<<24);}
static void put(uint8_t*p,uint64_t x,int n){for(int i=0;i<n;i++)p[i]=(x>>(i*8))&255;}
static struct sock_filter* filter_program(uint32_t peer){
 static struct sock_filter f[]={
  BPF_STMT(BPF_LD|BPF_W|BPF_ABS,(uint32_t)(SKF_AD_OFF+SKF_AD_PROTOCOL)),
  BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,ETH_P_IP,0,7),
  BPF_STMT(BPF_LD|BPF_B|BPF_ABS,9),BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,17,0,5),
  BPF_STMT(BPF_LD|BPF_W|BPF_ABS,12),BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,0,2,0),
  BPF_STMT(BPF_LD|BPF_W|BPF_ABS,16),BPF_JUMP(BPF_JMP|BPF_JEQ|BPF_K,0,0,1),
  BPF_STMT(BPF_RET|BPF_K,96),BPF_STMT(BPF_RET|BPF_K,0)};
 f[5].k=f[7].k=ntohl(peer);return f;
}
/* Offline interpreter of the exact limited instruction set used above. */
static uint32_t test_filter(const uint8_t*p,size_t n,unsigned protocol,uint32_t peer){
 struct sock_filter*f=filter_program(peer);uint32_t a=0;
 for(unsigned i=0;i<10;i++){
  struct sock_filter v=f[i];if(v.code==(BPF_RET|BPF_K))return v.k;
  if(v.code==(BPF_JMP|BPF_JEQ|BPF_K)){i+=a==v.k?v.jt:v.jf;continue;}
  if(v.k==(uint32_t)(SKF_AD_OFF+SKF_AD_PROTOCOL)){a=protocol;continue;}
  unsigned size=v.code==(BPF_LD|BPF_B|BPF_ABS)?1:4;if(v.k+size>n)return 0;
  a=0;for(unsigned k=0;k<size;k++)a=(a<<8)|p[v.k+k];
 }return 0;
}

/* Fixed 40-byte metadata only. No packet data/address is written. */
static int parse(const uint8_t*p,size_t n,int outgoing,uint32_t peer,uint8_t*r){
 if(n<28||(p[0]>>4)!=4||p[9]!=17||(be16(p+6)&0x3fff))return 0;
 unsigned h=(p[0]&15)*4;if(h<20||h+12>n)return 0;
 uint32_t a;memcpy(&a,p+(outgoing?16:12),4);if(a!=peer)return 0;
 unsigned total=be16(p+2),u=be16(p+h+4);if(u<12||total<h+u)return 0;
 const uint8_t*b=p+h+8;size_t cap=n-h-8;unsigned bytes=u-8;
 if(b[0]!=0xf1||be16(b+2)!=bytes-4)return 0;
 memset(r,0,40);put(r+16,be16(p+h+(outgoing?0:2)),2);put(r+18,be16(p+h+(outgoing?2:0)),2);put(r+20,bytes,2);
 r[26]=outgoing;r[27]=b[1];r[28]=255;r[29]=255;put(r+22,65535,2);
 if(b[1]==0xd0){if(bytes<8||cap<8)return 0;r[28]=b[5];put(r+22,be16(b+6),2);
  if(bytes>=24&&cap>=24&&!memcmp(b+8,"XZYH",4)){put(r+24,le16(b+12),2);r[29]=b[20];r[30]=b[21];put(r+32,le32(b+14),4);r[31]=1;}
 }else if(b[1]==0xd1){if(bytes<10||cap<10)return 0;unsigned c=be16(b+6);if(!c||c>4096||8+2*c>bytes)return 0;r[28]=b[5];put(r+22,be16(b+8),2);put(r+36,c,2);}
 return 1;
}
static size_t fixture(uint8_t*p,uint32_t peer,int out){
 memset(p,0,96);p[0]=0x45;p[9]=17;memcpy(p+(out?16:12),&peer,4);p[3]=52;p[20]=0x30;p[21]=0x39;p[22]=0x40;p[23]=0;p[25]=32;
 uint8_t*b=p+28;b[0]=0xf1;b[1]=0xd0;b[3]=20;b[5]=1;b[6]=0x12;b[7]=0x34;memcpy(b+8,"XZYH",4);b[12]=0x09;b[13]=0x05;b[14]=5;b[20]=2;b[21]=8;return 52;
}
static int selftest(void){uint8_t p[96],r[40];uint32_t peer;inet_pton(AF_INET,"192.0.2.1",&peer);size_t n=fixture(p,peer,0);
 if(test_filter(p,n,ETH_P_IP,peer)!=96||test_filter(p,n,ETH_P_IPV6,peer))return 9;
 if(!parse(p,n,0,peer,r)||le16(r+16)!=16384||le16(r+22)!=0x1234||le16(r+24)!=1289||r[29]!=2||r[30]!=8||le32(r+32)!=5)return 1;
 for(size_t k=0;k<36;k++)if(parse(p,k,0,peer,r))return 2;
 p[6]=0x20;if(parse(p,n,0,peer,r))return 3;p[6]=0;p[9]=6;if(parse(p,n,0,peer,r)||test_filter(p,n,ETH_P_IP,peer))return 4;p[9]=17;
 uint32_t other;inet_pton(AF_INET,"192.0.2.2",&other);if(parse(p,n,0,other,r)||test_filter(p,n,ETH_P_IP,other))return 5;
 fixture(p,peer,1);if(!parse(p,n,1,peer,r)||le16(r+16)!=12345)return 6;
 p[29]=0xd1;p[31]=6;p[3]=38;p[25]=18;p[34]=0;p[35]=1;p[36]=0xab;p[37]=0xcd;if(!parse(p,38,1,peer,r)||le16(r+36)!=1||le16(r+22)!=0xabcd)return 7;
 p[35]=2;if(parse(p,38,1,peer,r))return 8;
 puts("selftest: header/ports/command/channel/sequence/ACK/bounds/fragments/nonUDP/nonpeer passed");return 0;
}
int main(int argc,char**argv){if(argc==2&&!strcmp(argv[1],"--selftest"))return selftest();if(argc!=5){fprintf(stderr,"usage: IFACE PEER SECONDS OUT\n");return 1;}
 uint32_t peer;if(inet_pton(AF_INET,argv[2],&peer)!=1)return 2;unsigned seconds=atoi(argv[3]);if(!seconds||seconds>45)return 2;
 int s=socket(AF_PACKET,SOCK_DGRAM,htons(ETH_P_ALL));if(s<0){perror("socket");return 3;}
 struct sockaddr_ll bindaddr={.sll_family=AF_PACKET,.sll_protocol=htons(ETH_P_ALL),.sll_ifindex=if_nametoindex(argv[1])};if(!bindaddr.sll_ifindex||bind(s,(void*)&bindaddr,sizeof(bindaddr))){perror("bind");return 4;}
 // Filter IPv4 UDP to/from peer, then retain at most 96 header bytes.
 struct sock_fprog program={.len=10,.filter=filter_program(peer)};int one=1,buf=1024*1024;
 if(setsockopt(s,SOL_SOCKET,SO_ATTACH_FILTER,&program,sizeof(program))||setsockopt(s,SOL_SOCKET,SO_TIMESTAMPNS,&one,sizeof(one))){perror("options");return 5;}
 setsockopt(s,SOL_SOCKET,SO_RCVBUF,&buf,sizeof(buf));socklen_t bl=sizeof(buf);getsockopt(s,SOL_SOCKET,SO_RCVBUF,&buf,&bl);
 FILE*out=fopen(argv[4],"wb");if(!out){perror("out");return 6;}setvbuf(out,NULL,_IOFBF,65536);
 uint64_t start=ns(CLOCK_MONOTONIC),wall=ns(CLOCK_REALTIME),cpu=ns(CLOCK_PROCESS_CPUTIME_ID),end=start+(uint64_t)seconds*1000000000,records=0,received=0,missing=0,trunc=0;
 int io_error=0;
 uint8_t packets[32][96];char controls[32][CMSG_SPACE(sizeof(struct timespec))];struct sockaddr_ll addresses[32];struct iovec ios[32];struct mmsghdr messages[32];
 while(!io_error&&ns(CLOCK_MONOTONIC)<end){struct pollfd pf={.fd=s,.events=POLLIN};int rc=poll(&pf,1,100);if(rc<=0){if(rc<0&&errno!=EINTR){io_error=1;break;}continue;}
  for(unsigned j=0;j<32;j++){ios[j]=(struct iovec){packets[j],96};memset(&messages[j],0,sizeof(messages[j]));messages[j].msg_hdr=(struct msghdr){.msg_name=&addresses[j],.msg_namelen=sizeof(addresses[j]),.msg_iov=&ios[j],.msg_iovlen=1,.msg_control=controls[j],.msg_controllen=sizeof(controls[j])};}
  int count=recvmmsg(s,messages,32,MSG_DONTWAIT,NULL);if(count<0){if(errno!=EAGAIN&&errno!=EINTR)io_error=1;continue;}
  for(int j=0;j<count;j++){uint8_t r[40];struct msghdr*m=&messages[j].msg_hdr;received++;if(m->msg_flags&(MSG_TRUNC|MSG_CTRUNC)){trunc++;continue;}uint64_t stamp=0;
   for(struct cmsghdr*c=CMSG_FIRSTHDR(m);c;c=CMSG_NXTHDR(m,c))if(c->cmsg_level==SOL_SOCKET&&c->cmsg_type==SCM_TIMESTAMPNS){struct timespec t;memcpy(&t,CMSG_DATA(c),sizeof(t));stamp=(uint64_t)t.tv_sec*1000000000+t.tv_nsec;}
   if(!stamp){missing++;continue;}if(addresses[j].sll_protocol!=htons(ETH_P_IP)||!parse(packets[j],messages[j].msg_len,addresses[j].sll_pkttype==PACKET_OUTGOING,peer,r))continue;
   put(r,stamp,8);put(r+8,ns(CLOCK_MONOTONIC),8);if(fwrite_unlocked(r,40,1,out)!=1){perror("write");io_error=1;break;}records++;
  }
 }
 uint64_t stop=ns(CLOCK_MONOTONIC),cpuend=ns(CLOCK_PROCESS_CPUTIME_ID);struct tpacket_stats stats={0};socklen_t sl=sizeof(stats);if(getsockopt(s,SOL_PACKET,PACKET_STATISTICS,&stats,&sl))io_error=1;if(fclose(out))io_error=1;close(s);
 fprintf(stderr,"{\"startWallNs\":%llu,\"startMonoNs\":%llu,\"endMonoNs\":%llu,\"elapsedNs\":%llu,\"cpuNs\":%llu,\"oneCoreCpuPercent\":%.4f,\"records\":%llu,\"received\":%llu,\"packetSocketPackets\":%u,\"packetSocketDrops\":%u,\"missingTimestamp\":%llu,\"truncations\":%llu,\"receiveBufferBytes\":%d,\"recordBytes\":40}\n",(unsigned long long)wall,(unsigned long long)start,(unsigned long long)stop,(unsigned long long)(stop-start),(unsigned long long)(cpuend-cpu),100.0*(cpuend-cpu)/(stop-start),(unsigned long long)records,(unsigned long long)received,stats.tp_packets,stats.tp_drops,(unsigned long long)missing,(unsigned long long)trunc,buf);
 return io_error||stats.tp_drops||missing||trunc?7:0;
}
