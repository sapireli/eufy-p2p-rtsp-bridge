/* Optional finite, metadata-only diagnostics. No decoded GstMemory is mapped. */
#ifndef EUFY_TIMING_OBSERVER_H
#define EUFY_TIMING_OBSERVER_H
#include <fcntl.h>
#include <sys/stat.h>
typedef struct {
  FILE *file;
  uint8_t clock[48];
  guint64 clock_read_at, records;
  gboolean clock_valid;
} TimingObserver;
static gboolean timing_open(TimingObserver *o, const gchar *path) {
  memset(o, 0, sizeof(*o));
  if (!path) return TRUE;
  int fd=open(path,O_WRONLY|O_CREAT|O_APPEND|O_CLOEXEC|O_NOFOLLOW,0600);
  if (fd<0) return FALSE;
  struct stat st;
  if (fstat(fd,&st) || !S_ISREG(st.st_mode) || st.st_size>=64*1024*1024) {
    close(fd); return FALSE;
  }
  o->file=fdopen(fd,"a");
  if (!o->file) {close(fd);return FALSE;}
  setvbuf(o->file,NULL,_IOLBF,0);
  return TRUE;
}
static void timing_close(TimingObserver *o) {
  if (o->file) fclose(o->file);
  o->file=NULL;
}
static void timing_record(TimingObserver *o, const gchar *clock_path,
    const char *phase, guint channel, guint64 pts, guint64 now,
    const FreshStamp *stamp, gboolean found, gsize bytes, gint width, gint height) {
  if (!o->file) return;
  struct stat st;
  if (o->records>=40000 || fstat(fileno(o->file),&st) || st.st_size>=64*1024*1024) {
    g_printerr("eufy-timing channel=%u diagnostic limit reached; playback continues\n",channel);
    timing_close(o); return;
  }
  if (found && clock_path && (!o->clock_valid || now-o->clock_read_at>=100000000 ||
      memcmp(o->clock+8,stamp->epoch,8))) {
    FILE *f=fopen(clock_path,"rb");
    o->clock_valid=f && fread(o->clock,1,48,f)==48;
    if(f)fclose(f);
    o->clock_read_at=now;
  }
  guint64 lower=0,upper=0,uncertainty=0;
  gboolean valid=found && o->clock_valid &&
      fresh_age_bounds(stamp,o->clock,now,&lower,&upper,&uncertainty);
  uint8_t empty[8]={0};const uint8_t *epoch=found?stamp->epoch:empty;
  int n=fprintf(o->file,
      "{\"phase\":\"%s\",\"channel\":%u,\"monoNs\":\"%"G_GUINT64_FORMAT
      "\",\"ptsNs\":\"%"G_GUINT64_FORMAT"\",\"stampFound\":%s,"
      "\"epoch\":\"%02x%02x%02x%02x%02x%02x%02x%02x\","
      "\"frameId\":\"%"G_GUINT64_FORMAT"\",\"sourceTimestamp\":%u,"
      "\"clockValid\":%s,\"ageLowerNs\":\"%"G_GUINT64_FORMAT
      "\",\"ageUpperNs\":\"%"G_GUINT64_FORMAT"\",\"uncertaintyNs\":\"%"G_GUINT64_FORMAT
      "\",\"bytes\":%"G_GSIZE_FORMAT",\"width\":%d,\"height\":%d}\n",
      phase,channel,now,pts,found?"true":"false",epoch[0],epoch[1],epoch[2],epoch[3],
      epoch[4],epoch[5],epoch[6],epoch[7],found?stamp->frame:0,
      found?stamp->source:0,valid?"true":"false",lower,upper,uncertainty,bytes,width,height);
  o->records++;
  if(n<0) {g_printerr("eufy-timing write failed; playback continues\n");timing_close(o);}
}
#endif
