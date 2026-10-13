/* Read-only five-second process CPU census; no command lines or payloads.
 * Build: cc -O2 -Wall -o /tmp/cpu-census cpu_census_probe.c
 * Pure parser check: /tmp/cpu-census --self-test
 * Capture: /tmp/cpu-census > /tmp/cpu-census.txt
 * Reads /proc/PID/stat once at each endpoint and guards PID reuse by startticks.
 * Concurrent /dev/kmsg metadata counter seeks end first; retains no messages.
 * available/complete distinguish unavailable/lost evidence from zero warnings.
 * Global IRQ/softIRQ counters overlap per-process system accounting; do not add
 * them as independent workload buckets. Missing/exited process work is unknown.
 * CPU fields use CLK_TCK; census read intervals are not instantaneous.
 */
#define _GNU_SOURCE
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <dirent.h>
#include <unistd.h>
#include <time.h>
#include <fcntl.h>
#include <stdint.h>
#include <errno.h>
#include <poll.h>
#include <limits.h>
#include <sys/resource.h>
#define CAP 2048
typedef struct{
  int pid,ppid;
  char comm[64];
  unsigned long long user,system,start;
}
Row;
typedef struct{
  Row rows[CAP];
  int n,truncated;
  long long begin,end;
  unsigned long long cpu[8],rx,tx;
}
Snapshot;
static long long ns(clockid_t id){
  struct timespec t;
  clock_gettime(id,&t);
  return (long long)t.tv_sec*1000000000+t.tv_nsec;
}
typedef struct {
  int fd,available,error;
  unsigned long long records,warnings,epipe,seqGaps,malformed,lastSeq;
  int haveSeq,bounded;
  long long seekMonoNs;
} KernelCounts;
/* Kernel record text is used transiently, never copied to output/artifacts. */
static int krecord(KernelCounts *k,char *b){
  unsigned long long seq,stamp;
  unsigned int pri;
  int consumed=0;
  if(sscanf(b,"%u,%llu,%llu,%*[^;];%n",&pri,&seq,&stamp,&consumed)!=3||!consumed){
    k->malformed++; return 0;
  }
  k->records++;
  if(k->haveSeq && seq>k->lastSeq && seq-k->lastSeq>1)k->seqGaps+=seq-k->lastSeq-1;
  if(k->haveSeq && seq<=k->lastSeq)k->malformed++;
  k->lastSeq=seq;k->haveSeq=1;
  char *message=b+consumed,*newline=strchr(message,'\n');
  if(newline)*newline=0;
  /* Only the WARNING headline, not the later function/stack entries. */
  if(!strncmp(message,"WARNING:",8)&&strstr(message,"brcmf_fws_rxreorder"))k->warnings++;
  return 1;
}
static KernelCounts kopen(void){
  KernelCounts k={.fd=-1};
  k.fd=open("/dev/kmsg",O_RDONLY|O_NONBLOCK|O_CLOEXEC);
  if(k.fd<0){k.error=errno;return k;}
  if(lseek(k.fd,0,SEEK_END)<0){k.error=errno;close(k.fd);k.fd=-1;return k;}
  k.available=1;k.seekMonoNs=ns(CLOCK_MONOTONIC);return k;
}
static void kdrain(KernelCounts*k,int limit){
  if(k->fd<0)return;
  for(int i=0;i<limit;i++){
    char buf[8192];ssize_t n=read(k->fd,buf,sizeof buf-1);
    if(n>0){buf[n]=0;krecord(k,buf);continue;}
    if(n==0){k->error=EIO;close(k->fd);k->fd=-1;return;}
    if(errno==EPIPE){k->epipe++;continue;}
    if(errno==EINTR){i--;continue;}
    if(errno!=EAGAIN){k->error=errno;close(k->fd);k->fd=-1;}
    return;
  }
  /* Drain cap protects observer cost; not a proven loss count. */
  k->bounded++;
}
static void kwait(KernelCounts*k,long long deadline){
  while(ns(CLOCK_MONOTONIC)<deadline){
    long long left=deadline-ns(CLOCK_MONOTONIC);
    if(k->fd<0){
      struct timespec w={left/1000000000,left%1000000000};
      nanosleep(&w,NULL);continue;
    }
    struct pollfd fd={.fd=k->fd,.events=POLLIN};
    int ms=(int)((left+999999)/1000000);
    int rc=poll(&fd,1,ms);
    if(rc>0){kdrain(k,128);}
    else if(rc<0&&errno!=EINTR){k->error=errno;close(k->fd);k->fd=-1;}
  }
}
static int readtext(const char*p,char*b,size_t size){
  int f=open(p,O_RDONLY);
  if(f<0)return 0;
  int n=read(f,b,size-1);
  close(f);
  if(n<0)return 0;
  b[n]=0;
  return n;
}
static int parse(char*b,Row*r){
  char*l=strchr(b,'('),*q=strrchr(b,')'),*save;
  if(!l||!q||q<=l)return 0;
  size_t n=q-l-1;
  if(n>=sizeof r->comm)n=sizeof r->comm-1;
  memcpy(r->comm,l+1,n);
  r->comm[n]=0;
  int field=3;
  for(char*t=strtok_r(q+2," ",&save);t;t=strtok_r(NULL," ",&save),field++){
    if(field==4)r->ppid=atoi(t);
    if(field==14)r->user=strtoull(t,0,10);
    if(field==15)r->system=strtoull(t,0,10);
    if(field==22)r->start=strtoull(t,0,10);
  }
  return r->start!=0;
}
static void take(Snapshot*s){
  char b[4096],p[128];
  s->begin=ns(CLOCK_MONOTONIC);
  if(readtext("/proc/stat",b,sizeof b))sscanf(b,"cpu %llu %llu %llu %llu %llu %llu %llu %llu",s->cpu,s->cpu+1,s->cpu+2,s->cpu+3,s->cpu+4,s->cpu+5,s->cpu+6,s->cpu+7);
  FILE*f=fopen("/proc/net/dev","r");
  if(f){
    while(fgets(b,sizeof b,f)){
      char*z=strstr(b,"wlan0:");
      if(z){
        unsigned long long v[16];
        if(sscanf(z+6,"%llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu %llu",v,v+1,v+2,v+3,v+4,v+5,v+6,v+7,v+8,v+9,v+10,v+11,v+12,v+13,v+14,v+15)==16){
          s->rx=v[0];
          s->tx=v[8];
        }
        break;
      }
    }
    fclose(f);
  }
  DIR*d=opendir("/proc");
  if(d){
    struct dirent*de;
    while((de=readdir(d))){
      int pid=atoi(de->d_name);
      if(!pid)continue;
      if(s->n==CAP){
        s->truncated=1;
        break;
      }
      snprintf(p,sizeof p,"/proc/%d/stat",pid);
      Row r={
        .pid=pid
      }
      ;
      if(readtext(p,b,sizeof b)&&parse(b,&r))s->rows[s->n++]=r;
    }
    closedir(d);
  }
  s->end=ns(CLOCK_MONOTONIC);
}
static Row*find(Snapshot*s,int pid){
  for(int i=0;i<s->n;i++)if(s->rows[i].pid==pid)return s->rows+i;
  return NULL;
}
static int ownlineage(Snapshot*s,int pid){
  int me=getpid();
  for(int i=0;i<20&&me>1;i++){
    if(pid==me)return 1;
    Row*r=find(s,me);
    if(!r)break;
    me=r->ppid;
  }
  return 0;
}
static int excluded(Snapshot*s,Row*r){
  return ownlineage(s,r->pid)||!strncmp(r->comm,"sshd",4)||!strcmp(r->comm,"cc")||!strcmp(r->comm,"cc1")||!strcmp(r->comm,"as")||!strcmp(r->comm,"ld");
}
int main(int argc,char**argv){
  if(argc==2&&!strcmp(argv[1],"--self-test")){
    char sample[]="1 (name with ) space) S 42 0 0 0 0 0 0 0 0 0 111 222 0 0 0 0 0 0 999";
    Row r={
      0
    }
    ;
    if(!parse(sample,&r)||strcmp(r.comm,"name with ) space")||r.ppid!=42||r.user!=111||r.system!=222||r.start!=999)return 1;
    KernelCounts k={0};
    char warn[]="4,10,123,-;WARNING: CPU: 0 at fwsignal.c:1731 brcmf_fws_rxreorder+0x20/0x30\n extra";
    char stack[]="4,11,124,-; brcmf_fws_rxreorder+0x20/0x30";
    char other[]="4,14,125,-;WARNING: CPU: 0 other_function";
    char bad[]="invalid;WARNING: brcmf_fws_rxreorder";
    char property[]="4,15,126,-;unrelated\n WARNING: brcmf_fws_rxreorder";
    if(!krecord(&k,warn)||!krecord(&k,stack)||!krecord(&k,other)||krecord(&k,bad)||!krecord(&k,property)||
       k.records!=4||k.warnings!=1||k.seqGaps!=2||k.malformed!=1)return 2;
    char duplicate[]="4,15,127,-;unrelated";
    if(!krecord(&k,duplicate)||k.malformed!=2||k.warnings!=1)return 3;
    KernelCounts fresh={0};
    char first[]="4,999,128,-;WARNING: brcmf_fws_rxreorder";
    if(!krecord(&fresh,first)||fresh.seqGaps||fresh.warnings!=1)return 4;
    puts("self-test process stat and metadata-only kernel warning parsing PASS");
    return 0;
  }
  static Snapshot a,b;
  struct rlimit nocore={0,0};
  setrlimit(RLIMIT_CORE,&nocore);
  long long cpu0=ns(CLOCK_PROCESS_CPUTIME_ID);
  KernelCounts kernel=kopen();
  long long start=kernel.available?kernel.seekMonoNs:ns(CLOCK_MONOTONIC);
  take(&a);
  kwait(&kernel,start+5000000000);
  take(&b);
  kdrain(&kernel,4096);
  if(kernel.fd>=0)close(kernel.fd);
  long long cpu=ns(CLOCK_PROCESS_CPUTIME_ID)-cpu0,end=ns(CLOCK_MONOTONIC);
  printf("scope startMonoNs=%lld endMonoNs=%lld firstSnapshotMonoNs=%lld lastSnapshotMonoNs=%lld elapsedNs=%lld observerCpuNs=%lld observerCpuPct=%.6f clkTck=%ld firstReadNs=%lld lastReadNs=%lld truncated=%d,%d\n",start,end,a.begin,b.begin,end-start,cpu,100.0*cpu/(end-start),sysconf(_SC_CLK_TCK),a.end-a.begin,b.end-b.begin,a.truncated,b.truncated);
  printf("kernel_warning_counter available=%d complete=%d seekMonoNs=%lld endMonoNs=%lld records=%llu rxreorderWarningHeadlines=%llu epipeEvents=%llu sequenceMissingRecords=%llu malformedRecords=%llu drainCapHits=%d errorErrno=%d\n",
         kernel.available,kernel.available&&!kernel.error&&!kernel.epipe&&!kernel.seqGaps&&!kernel.malformed&&!kernel.bounded,
         kernel.seekMonoNs,end,kernel.records,kernel.warnings,kernel.epipe,kernel.seqGaps,kernel.malformed,kernel.bounded,kernel.error);
  printf("global_delta user=%llu nice=%llu system=%llu idle=%llu iowait=%llu irq=%llu softirq=%llu steal=%llu rxBytes=%llu txBytes=%llu\n",b.cpu[0]-a.cpu[0],b.cpu[1]-a.cpu[1],b.cpu[2]-a.cpu[2],b.cpu[3]-a.cpu[3],b.cpu[4]-a.cpu[4],b.cpu[5]-a.cpu[5],b.cpu[6]-a.cpu[6],b.cpu[7]-a.cpu[7],b.rx-a.rx,b.tx-a.tx);
  int matched=0,gone=0,fresh=0;
  for(int i=0;i<a.n;i++){
    Row*x=a.rows+i,*y=find(&b,x->pid);
    if(!y||y->start!=x->start){
      gone++;
      continue;
    }
    matched++;
    printf("process pid=%d comm=%s start=%llu userTicks=%llu systemTicks=%llu excludedObserverCompilerSSH=%d\n",x->pid,x->comm,x->start,y->user-x->user,y->system-x->system,excluded(&a,x));
  }
  for(int i=0;i<b.n;i++){
    Row*x=b.rows+i,*y=find(&a,x->pid);
    if(!y||y->start!=x->start)fresh++;
  }
  printf("identities first=%d last=%d matched=%d disappearedOrReused=%d newOrReused=%d\n",a.n,b.n,matched,gone,fresh);
  return 0;
}
