/* Metadata-only ARM32 RTSP reader observer.
 * Build: cc -O2 -Wall -o /tmp/rtsp-reader-probe rtsp_reader_probe.c
 * Check authorization guards: /tmp/rtsp-reader-probe --self-test
 * Run as root with existing Gst PIDs: /tmp/rtsp-reader-probe PID PID
 * --extend30 permits 30 additional seconds ONLY after both direct socket
 * identities and the <=2% five-second observer CPU gate pass.
 * Does not start/stop services or modify playback. Physical output unobserved.
 * /proc/PID/mem is read only for EXACTLY16 bytes at a validated ARM32 poll(2)
 * descriptor-array argument (nfds==2), with same syscall args checked afterward.
 * No arbitrary memory, encoded-video bytes, decoded pixels or socket payloads.
 * State, syscall and socket reads are non-atomic; do not infer a blocking stack.
 */
#define _GNU_SOURCE
#define _FILE_OFFSET_BITS 64
#include <stdio.h>
#include <stdlib.h>
#include <stdint.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <dirent.h>
#include <time.h>
#include <poll.h>
#include <errno.h>
_Static_assert(sizeof(struct pollfd)==8,"pollfd layout must be exactly8bytes");
typedef struct{
  int pid,tid,proven;
  unsigned long long pidStart,tidStart,provenInode;
}
Target;
static long long ns(clockid_t c){
  struct timespec t;
  clock_gettime(c,&t);
  return (long long)t.tv_sec*1000000000+t.tv_nsec;
}
static int text(const char *p,char *b,size_t n){
  int f=open(p,O_RDONLY);
  if(f<0)return -1;
  int r=read(f,b,n-1);
  close(f);
  if(r<0)return -1;
  b[r]=0;
  return r;
}
static int st(int pid,int tid,char *state,unsigned long long *ticks,unsigned long long *start){
  char p[128],b[4096],*save,*s;
  snprintf(p,sizeof p,"/proc/%d/task/%d/stat",pid,tid);
  if(text(p,b,sizeof b)<0)return -1;
  s=strrchr(b,')');
  if(!s)return -1;
  int n=3;
  *ticks=0;
  *start=0;
  for(char *t=strtok_r(s+2," ",&save);t;t=strtok_r(NULL," ",&save),n++){
    if(n==3)*state=t[0];
    if(n==14||n==15)*ticks+=strtoull(t,NULL,10);
    if(n==22)*start=strtoull(t,NULL,10);
  }
  return *start?0:-1;
}
static int syscallp(Target*t,char*b,size_t n){
  char p[128];
  snprintf(p,sizeof p,"/proc/%d/task/%d/syscall",t->pid,t->tid);
  return text(p,b,n);
}
static int socketq(int pid,int fd,unsigned long long *inode,unsigned *rx){
  char p[128],l[128],b[1024];
  snprintf(p,sizeof p,"/proc/%d/fd/%d",pid,fd);
  ssize_t n=readlink(p,l,sizeof l-1);
  if(n<0)return 0;
  l[n]=0;
  if(sscanf(l,"socket:[%llu]",inode)!=1)return 0;
  FILE*f=fopen("/proc/net/tcp","r");
  if(!f)return 0;
  int ok=0;
  while(fgets(b,sizeof b,f)){
    char*s,*v[12];
    int k=0;
    for(s=strtok(b," \t\n");s&&k<12;s=strtok(NULL," \t\n"))v[k++]=s;
    if(k<10||strtoull(v[9],NULL,10)!=*inode||strcmp(v[3],"01"))continue;
    char*c=strchr(v[2],':');
    if(!c||strcmp(c+1,"216A"))continue;
    unsigned tx;
    if(sscanf(v[4],"%x:%x",&tx,rx)==2)ok=1;
    break;
  }
  fclose(f);
  return ok;
}
static int pollargs(const char*s,unsigned long long*ptr){
  long call;
  unsigned long long n,p;
  if(sscanf(s,"%ld %llx %llx",&call,&p,&n)!=3||call!=168||n!=2||p<65536||p>0xbffffff0ULL)return 0;
  *ptr=p;
  return 1;
}
static int proof(Target*t){
  char a[512],b[512],p[128];
  unsigned long long ptr,n,ptr2,n2;
  long call,call2;
  if(syscallp(t,a,sizeof a)<0||!pollargs(a,&ptr))return 0;
  call=168;
  n=2;
  struct pollfd fds[2];
  snprintf(p,sizeof p,"/proc/%d/mem",t->pid);
  int f=open(p,O_RDONLY);
  if(f<0){
    printf("proof_error pid=%d errno=%d\n",t->pid,errno);
    return 0;
  }
  ssize_t r=pread(f,fds,16,(off_t)ptr);
  close(f);
  if(r!=16||syscallp(t,b,sizeof b)<0||sscanf(b,"%ld %llx %llx",&call2,&ptr2,&n2)!=3||call2!=call||ptr2!=ptr||n2!=n)return 0;
  for(int i=0;i<2;i++){
    if(fds[i].fd<0||fds[i].fd>4096)return 0;
  }
  printf("pollfd_metadata pid=%d tid=%d bytes=16 pointer_stable=true fd0=%d events0=%d revents0=%d fd1=%d events1=%d revents1=%d\n",t->pid,t->tid,fds[0].fd,fds[0].events,fds[0].revents,fds[1].fd,fds[1].events,fds[1].revents);
  for(int i=0;i<2;i++){
    unsigned long long inode;
    unsigned rx;
    if(socketq(t->pid,fds[i].fd,&inode,&rx)){
      printf("reader_proven pid=%d tid=%d fd=%d inode=%llu rtsp_port=8554 rx=%u\n",t->pid,t->tid,fds[i].fd,inode,rx);
      t->proven=fds[i].fd;
      t->provenInode=inode;
      return 1;
    }
  }
  return 0;
}
int main(int argc,char**argv){
  if(argc==2&&!strcmp(argv[1],"--self-test")){
    unsigned long long p;
    const char*bad[]={
      "running","168 0xb0000000 0x3","336 0xb0000000 0x2","168 0x1 0x2","168 0xc0000000 0x2","168 0xbfffffff 0x2"
    }
    ;
    for(unsigned i=0;i<sizeof bad/sizeof bad[0];i++)if(pollargs(bad[i],&p))return 1;
    if(!pollargs("168 0xb0000000 0x2",&p)||p!=0xb0000000ULL)return 1;
    puts("self-test strict pollfd authorization PASS");
    return 0;
  }
#if !defined(__arm__)
  fputs("unsupported platform: metadata poll parser is ARM32 only\n",stderr);
  return 2;
#endif
  int extend=argc>1&&!strcmp(argv[1],"--extend30");
  if(extend){
    argv++;
    argc--;
  }
  setvbuf(stdout,NULL,_IOLBF,0);
  long long cpu0=ns(CLOCK_PROCESS_CPUTIME_ID),wall0=ns(CLOCK_MONOTONIC);
  Target ts[8];
  int count=0;
  for(int z=1;z<argc&&count<8;z++){
    int pid=atoi(argv[z]);
    char p[128],b[256],state;
    unsigned long long tick,start;
    snprintf(p,sizeof p,"/proc/%d/comm",pid);
    if(text(p,b,sizeof b)<0||strcmp(b,"gst-launch-1.0\n")){
      printf("abort invalid_pid=%d\n",pid);
      return 2;
    }
    if(st(pid,pid,&state,&tick,&start))return 2;
    snprintf(p,sizeof p,"/proc/%d/task",pid);
    DIR*d=opendir(p);
    if(!d)return 2;
    struct dirent*de;
    int found=0;
    Target t={
      .pid=pid,.pidStart=start
    }
    ;
    while((de=readdir(d))){
      int tid=atoi(de->d_name);
      if(!tid)continue;
      snprintf(p,sizeof p,"/proc/%d/task/%d/comm",pid,tid);
      if(text(p,b,sizeof b)>0&&!strcmp(b,"task0\n")){
        found++;
        t.tid=tid;
        if(st(pid,tid,&state,&tick,&t.tidStart))return 2;
      }
    }
    closedir(d);
    if(found!=1){
      printf("abort pid=%d task0_count=%d\n",pid,found);
      return 2;
    }
    ts[count++]=t;
    printf("inventory pid=%d tid=%d pidStart=%llu tidStart=%llu\n",pid,t.tid,t.pidStart,t.tidStart);
  }
  if(!count){
    fputs("no targets\n",stderr);
    return 2;
  }
  for(int i=0;i<=(extend?35:5);i++){
    long long target=wall0+(long long)i*1000000000,now=ns(CLOCK_MONOTONIC);
    if(now<target){
      struct timespec wait={
        .tv_sec=(target-now)/1000000000,.tv_nsec=(target-now)%1000000000
      }
      ;
      nanosleep(&wait,NULL);
    }
    for(int z=0;z<count;z++){
      Target*t=&ts[z];
      unsigned long long tick,start,pt;
      char state,p[128],w[128];
      if(st(t->pid,t->pid,&state,&pt,&start)||start!=t->pidStart){
        printf("abort pid_changed=%d\n",t->pid);
        return 2;
      }
      long long t0=ns(CLOCK_MONOTONIC);
      if(st(t->pid,t->tid,&state,&tick,&start)||start!=t->tidStart){
        printf("abort tid_changed=%d\n",t->tid);
        return 2;
      }
      snprintf(p,sizeof p,"/proc/%d/task/%d/wchan",t->pid,t->tid);
      if(text(p,w,sizeof w)<0)strcpy(w,"unavailable");
      w[strcspn(w,"\n")]=0;
      if(!t->proven)proof(t);
      unsigned long long ino=0;
      unsigned rx=0;
      int has=t->proven&&socketq(t->pid,t->proven,&ino,&rx);
      if(t->proven&&(!has||ino!=t->provenInode)){
        printf("abort socket_changed pid=%d fd=%d\n",t->pid,t->proven);
        return 2;
      }
      printf("sample i=%d mono=%lld pid=%d tid=%d state=%c ticks=%llu wchan=%s proven=%d rx=%u readNs=%lld\n",i,t0,t->pid,t->tid,state,tick,w,has,rx,ns(CLOCK_MONOTONIC)-t0);
    }
    long long elapsed=ns(CLOCK_MONOTONIC)-wall0,cpu=ns(CLOCK_PROCESS_CPUTIME_ID)-cpu0;
    if(i==5){
      int all=1;
      for(int z=0;z<count;z++)if(!ts[z].proven)all=0;
      printf("gate elapsedNs=%lld cpuNs=%lld cpuPct=%.6f pass=%d allReadersProven=%d\n",elapsed,cpu,100.0*cpu/elapsed,cpu*50<=elapsed,all);
      if(extend&&(!all||cpu*50>elapsed)){
        puts("abort extension rejected");
        return 3;
      }
    }
  }
  printf("end elapsedNs=%lld cpuNs=%lld\n",ns(CLOCK_MONOTONIC)-wall0,ns(CLOCK_PROCESS_CPUTIME_ID)-cpu0);
  return 0;
}
