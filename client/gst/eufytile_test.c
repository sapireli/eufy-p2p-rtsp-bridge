/* Offline ownership tests: no cameras, display or hardware decoder required. */
#include "gsteufytile.c"
#include <assert.h>
static gpointer release_victim(gpointer data) {
  GstEufyTile *victim=data;
  for(guint i=0;i<200;i++) {
    if(tile_yield_requested(victim)) {
      g_usleep(20000); /* Model completion of decoder transition to NULL. */
      tile_hardware_active(victim,FALSE);return NULL;
    }
    g_usleep(1000);
  }
  assert(!"victim never received release request");return NULL;
}
int main(int argc,char **argv) {
  gst_init(&argc,&argv);
  struct stat before_stat={0},after_stat={0};
  before_stat.st_ino=after_stat.st_ino=10;before_stat.st_size=after_stat.st_size=500;
  before_stat.st_mtime=after_stat.st_mtime=100;before_stat.st_ctime=after_stat.st_ctime=100;
  assert(tile_same_stat(&before_stat,&after_stat));
#ifdef __APPLE__
  after_stat.st_mtimespec.tv_nsec=1;
  assert(!tile_same_stat(&before_stat,&after_stat));
  after_stat.st_mtimespec.tv_nsec=0;after_stat.st_ctimespec.tv_nsec=1;
#else
  after_stat.st_mtim.tv_nsec=1;
  assert(!tile_same_stat(&before_stat,&after_stat));
  after_stat.st_mtim.tv_nsec=0;after_stat.st_ctim.tv_nsec=1;
#endif
  assert(!tile_same_stat(&before_stat,&after_stat));
  assert(tile_retry_delay(0,100,1000,2000,0)==100);
  assert(tile_retry_delay(100,100,1000,2000,0)==200);
  assert(tile_retry_delay(800,100,1000,2000,0)==1000);
  assert(tile_retry_delay(1000,100,1000,2000,2000000)==100);
  PacingClock final_clock={0};int64_t final_target=0;
  assert(pacing_target(&final_clock,0,0,0,&final_target) && final_target==PACING_BOUND_NS);
  GstClockTime first=GST_CLOCK_TIME_NONE,at=0;
  assert(tile_map_pts(0,0,&first,&at,FALSE)==0);
  assert(tile_map_pts(GST_SECOND,GST_SECOND,&first,&at,FALSE)==GST_SECOND);
  assert(tile_map_pts(1100*GST_MSECOND,10*GST_SECOND,&first,&at,FALSE)==10*GST_SECOND);
  assert(tile_map_pts(100*GST_SECOND,10100*GST_MSECOND,&first,&at,FALSE)==10100*GST_MSECOND);
  assert(tile_map_pts(0,10200*GST_MSECOND,&first,&at,TRUE)==10200*GST_MSECOND);
  assert(tile_map_pts(GST_CLOCK_TIME_NONE,10300*GST_MSECOND,&first,&at,FALSE)==10300*GST_MSECOND);
  GstEufyTile *requester=g_object_new(gst_eufy_tile_get_type(),NULL);
  GstEufyTile *victim=g_object_new(gst_eufy_tile_get_type(),NULL);
  requester->hardware_decoder=g_strdup("v4l2h264dec");requester->hardware_priority=10;
  victim->hardware_decoder=g_strdup("v4l2h264dec");victim->hardware_priority=20;victim->hardware_active=TRUE;
  tile_pool=g_list_prepend(tile_pool,victim);
  assert(!tile_reclaim(requester)); /* Lower priority cannot evict. */
  requester->hardware_priority=30;victim->hardware_pinned=TRUE;
  assert(!tile_reclaim(requester)); /* Pins cannot be evicted. */
  victim->hardware_pinned=FALSE;
  g_free(victim->hardware_decoder);victim->hardware_decoder=g_strdup("vah264dec");
  assert(!tile_reclaim(requester)); /* Unrelated hardware is not a resource. */
  g_free(victim->hardware_decoder);victim->hardware_decoder=g_strdup("v4l2h264dec");
  requester->hardware_priority=0;requester->hardware_pinned=TRUE;
  GThread *thread=g_thread_new("release-test",release_victim,victim);
  gint64 before=g_get_monotonic_time();assert(tile_reclaim(requester));
  assert(g_get_monotonic_time()-before>=20000);assert(!victim->hardware_active);
  g_thread_join(thread);
  tile_pool=g_list_remove(tile_pool,victim);
  gst_object_unref(victim);gst_object_unref(requester);
  g_print("eufytile ownership tests passed\n");return 0;
}
