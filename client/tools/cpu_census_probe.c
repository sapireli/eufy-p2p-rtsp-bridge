/* Read-only five-second process CPU census; no command lines or payloads.
 * Build: cc -O2 -Wall -o /tmp/cpu-census cpu_census_probe.c
 * Pure parser check: /tmp/cpu-census --self-test
 * Capture: /tmp/cpu-census > /tmp/cpu-census.txt
 * Reads /proc/PID/stat once at each endpoint and guards PID reuse by startticks.
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
    puts("self-test process stat parsing PASS");
    return 0;
  }
  static Snapshot a,b;
  long long cpu0=ns(CLOCK_PROCESS_CPUTIME_ID),start=ns(CLOCK_MONOTONIC);
  take(&a);
  long long remain=start+5000000000-ns(CLOCK_MONOTONIC);
  if(remain>0){
    struct timespec w={
      remain/1000000000,remain%1000000000
    }
    ;
    nanosleep(&w,NULL);
  }
  take(&b);
  long long cpu=ns(CLOCK_PROCESS_CPUTIME_ID)-cpu0,end=ns(CLOCK_MONOTONIC);
  printf("scope startMonoNs=%lld endMonoNs=%lld firstSnapshotMonoNs=%lld lastSnapshotMonoNs=%lld elapsedNs=%lld observerCpuNs=%lld observerCpuPct=%.6f clkTck=%ld firstReadNs=%lld lastReadNs=%lld truncated=%d,%d\n",start,end,a.begin,b.begin,end-start,cpu,100.0*cpu/(end-start),sysconf(_SC_CLK_TCK),a.end-a.begin,b.end-b.begin,a.truncated,b.truncated);
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
