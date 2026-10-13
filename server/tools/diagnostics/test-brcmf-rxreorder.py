#!/usr/bin/env python3
"""Execute unchanged official kernel functions with deterministic synthetic skb metadata.
Usage: test-brcmf-rxreorder.py official-v6.18.50-fwsignal.c
Network/source/driver/device changes are outside this offline regression.
"""
import hashlib,pathlib,subprocess,sys,tempfile
raw=pathlib.Path(sys.argv[1]).read_bytes()
assert hashlib.sha256(raw).hexdigest()=="e7de3c68cc4af9b4f26cb48dcd326403be77bf2fb1f2ba7d49ddc6c9791e32cd"
s=raw.decode(); funcs=s[s.index("static void brcmf_rxreorder_get_skb_list("):s.index("void brcmf_fws_hdrpull(")]
defs=s[s.index("#define BRCMF_RXREORDER_FLOWID_OFFSET"):s.index("#ifdef DEBUG",s.index("#define BRCMF_RXREORDER_FLOWID_OFFSET"))]
stub=r"""
#include <stdint.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
typedef uint8_t u8;
struct sk_buff {u8 cb[32];struct sk_buff*next;unsigned id;};
struct brcmf_skb_reorder_data {u8 reorder[10];};
struct sk_buff_head {struct sk_buff*head,*tail;unsigned count;};
struct brcmf_ampdu_rx_reorder {u8 flow_id,cur_idx,exp_idx,max_idx,pend_pkts;struct sk_buff*pktslots[];};
struct brcmf_pub {struct brcmf_ampdu_rx_reorder*reorder_flows[256];};
struct brcmf_if {struct brcmf_pub*drvr;};
static unsigned warnings,delivered,freed;
static unsigned delivery_ids[4096], freed_ids[4096];
#define WARN_ON(x) do {if(x)warnings++;}while(0)
#define brcmf_dbg(...) ((void)0)
#define bphy_err(d,...) ((void)(d))
#define GFP_ATOMIC 0
#define struct_size(p,m,n) (sizeof(*(p))+(n)*sizeof((p)->m[0]))
#define kzalloc(n,f) calloc(1,n)
#define kfree free
static void __skb_queue_head_init(struct sk_buff_head*q){memset(q,0,sizeof(*q));}
static void __skb_queue_tail(struct sk_buff_head*q,struct sk_buff*p){p->next=NULL;if(q->tail)q->tail->next=p;else q->head=p;q->tail=p;q->count++;}
static unsigned skb_queue_len(struct sk_buff_head*q){return q->count;}
static void __skb_unlink(struct sk_buff*p,struct sk_buff_head*q){q->head=p->next;if(!q->head)q->tail=NULL;q->count--;}
#define skb_queue_walk_safe(q,p,n) for(p=(q)->head;p&&((n=p->next),1);p=n)
static void brcmu_pkt_buf_free_skb(struct sk_buff*p){freed_ids[freed++]=p->id;free(p);}
static void brcmf_netif_rx(struct brcmf_if*i,struct sk_buff*p){(void)i;delivery_ids[delivered++]=p->id;free(p);}
"""
extended=r"""
static unsigned serial, invariant_errors;
static int patched;
static unsigned occupied(struct brcmf_ampdu_rx_reorder*r){unsigned n=0;for(unsigned k=0;k<=r->max_idx;k++)n+=r->pktslots[k]!=NULL;return n;}
static void step(struct brcmf_if*i,u8 flags,u8 cur,u8 exp,u8 max,u8 flow){
 struct sk_buff*p=calloc(1,sizeof(*p));p->id=++serial;p->cb[0]=flow;p->cb[2]=max;p->cb[4]=flags;p->cb[6]=cur;p->cb[8]=exp;
 brcmf_fws_rxreorder(i,p);
 struct brcmf_ampdu_rx_reorder*r=i->drvr->reorder_flows[flow];
 if(r&&r->pend_pkts!=occupied(r))invariant_errors++;
}
static void reset(void){serial=invariant_errors=warnings=delivered=freed=0;memset(delivery_ids,0,sizeof(delivery_ids));memset(freed_ids,0,sizeof(freed_ids));}
static int order(const unsigned*expect,unsigned n){return delivered==n&&!memcmp(delivery_ids,expect,n*sizeof(*expect));}
#define STEP(f,c,e) step(&i,f,c,e,7,0)
#define CHECK(x) do{if(!(x)){fprintf(stderr,"check failure line%d scenario%u patched%d\n",__LINE__,scenario,patched);return 1;}}while(0)
int main(int argc,char**argv){(void)argv;patched=argc>1;
 for(unsigned scenario=0;scenario<11;scenario++){
  struct brcmf_pub d={0};struct brcmf_if i={&d};reset();
  unsigned expect[8]={0},n=4;
  switch(scenario){
   case 0: STEP(0x10,2,1);STEP(0x04,3,1);STEP(0x04,1,4);STEP(1,0,0);{unsigned a[]={3,1,2,4};memcpy(expect,a,sizeof(a));}break;
   case 1: STEP(0x10,2,1);STEP(0x04,3,1);STEP(0x10,4,4);STEP(1,0,0);{unsigned a[]={1,2,3,4};memcpy(expect,a,sizeof(a));}break;
   case 2: STEP(0x10,2,1);STEP(0x04,4,5);STEP(1,0,0);n=3;{unsigned a[]={1,2,3};memcpy(expect,a,sizeof(a));}break;
   case 3: STEP(0x10,2,1);STEP(0x04,4,3);STEP(1,0,0);n=3;{unsigned a[]={1,2,3};memcpy(expect,a,sizeof(a));}break;
   case 4: STEP(0x10,2,1);STEP(0x04,4,1);STEP(0,0,3);STEP(1,0,0);{unsigned a[]={1,3,2,4};memcpy(expect,a,sizeof(a));}break;
   case 5: STEP(0x10,6,5);STEP(0x04,1,5);STEP(BRCMF_RXREORDER_FLUSH_ALL,0,2);STEP(1,0,0);{unsigned a[]={1,2,3,4};memcpy(expect,a,sizeof(a));}break;
   case 6: STEP(0x10,0,6);STEP(0x04,7,6);STEP(0x04,6,1);STEP(1,0,0);{unsigned a[]={3,2,1,4};memcpy(expect,a,sizeof(a));}break;
   case 7: STEP(0x10,2,1);STEP(0x04,2,1);STEP(0x10,4,4);STEP(1,0,0);n=3;{unsigned a[]={2,3,4};memcpy(expect,a,sizeof(a));}break;
   case 8: STEP(0x10,2,2);STEP(0x04,2,3);STEP(0x10,4,4);STEP(1,0,0);n=3;{unsigned a[]={2,3,4};memcpy(expect,a,sizeof(a));}break;
   case 10: STEP(0x10,6,5);STEP(0x04,1,5);STEP((BRCMF_RXREORDER_CURIDX_VALID|BRCMF_RXREORDER_FLUSH_ALL),3,4);STEP(1,0,0);{unsigned a[]={1,2,3,4};memcpy(expect,a,sizeof(a));}break;
   case 9: STEP(0xff,0,0);STEP(1,0,0);STEP(0x10,2,1);step(&i,0x10,0,3,3,0);STEP(1,0,0);n=5;{unsigned a[]={1,2,3,4,5};memcpy(expect,a,sizeof(a));}break;
  }
  CHECK(order(expect,n));CHECK(d.reorder_flows[0]==NULL);
  if(scenario==7||scenario==8){CHECK(freed==1&&freed_ids[0]==1);CHECK(patched?invariant_errors==0:invariant_errors>0);CHECK(warnings==(patched?0:1));}
  else {CHECK(!freed&&!warnings&&!invariant_errors);}
  printf("case=%u delivery-order-preserved packets=%u replaced=%u invariant-errors=%u\n",scenario,delivered,freed,invariant_errors);
 }
 // Repeated replacement beyond the u8 count range must not wrap candidate accounting.
 {unsigned scenario=11;struct brcmf_pub d={0};struct brcmf_if i={&d};reset();STEP(0x10,2,1);for(unsigned k=0;k<320;k++)STEP(0x04,2,1);STEP(0x10,4,4);STEP(1,0,0);
 unsigned a[]={321,322,323};CHECK(order(a,3));CHECK(freed==320);CHECK(patched?invariant_errors==0:invariant_errors>0);CHECK(warnings==(patched?0:1));printf("320replacementburst invariant-errors=%u replaced=%u\n",invariant_errors,freed);}
 // Third occupied-slot overwrite must free the old packet and balance its count.
 {unsigned scenario=12;struct brcmf_pub d={0};struct brcmf_if i={&d};reset();STEP(0x10,4,1);STEP(0x04,4,3);CHECK(invariant_errors==(patched?0:1));CHECK(d.reorder_flows[0]->pend_pkts==(patched?1:2)&&occupied(d.reorder_flows[0])==1);STEP(0x10,6,6);CHECK(warnings==(patched?0:1));STEP(1,0,0);unsigned a[]={2,3,4};CHECK(order(a,3));CHECK(freed==(patched?1:0));if(patched)CHECK(freed_ids[0]==1);printf("both-moved-overwrite: invariant-errors=%u replaced=%u patched=%d\n",invariant_errors,freed,patched);}
 // Full flushing must remove an occupied destination before the new storage guard sees it.
 {unsigned scenario=14;struct brcmf_pub d={0};struct brcmf_if i={&d};reset();STEP(0x10,4,1);STEP(0x04,2,1);STEP((BRCMF_RXREORDER_CURIDX_VALID|BRCMF_RXREORDER_FLUSH_ALL),4,3);STEP(1,0,0);unsigned a[]={2,1,3,4};CHECK(order(a,4));CHECK(!freed&&!warnings&&!invariant_errors);puts("both-moved fullflush storage: prior packets delivered, not freed twice");}
 // A partial flush must deliver its own range while replacing only the occupied destination.
 {unsigned scenario=15;struct brcmf_pub d={0};struct brcmf_if i={&d};reset();STEP(0x10,4,1);STEP(0x04,2,1);STEP(0x04,4,3);STEP(0x10,6,6);STEP(1,0,0);unsigned a[]={2,3,4,5};CHECK(order(a,4));CHECK(freed==(patched?1:0));if(patched)CHECK(freed_ids[0]==1);CHECK(patched?invariant_errors==0:invariant_errors>0);CHECK(warnings==(patched?0:1));printf("both-moved partialflush storage: replaced=%u invariant-errors=%u\n",freed,invariant_errors);}
 // Independent firmware flows must not share counters or delivery state.
 {unsigned scenario=13;struct brcmf_pub d={0};struct brcmf_if i={&d};reset();step(&i,0x10,2,1,7,0);step(&i,0x10,3,2,7,1);step(&i,1,0,0,7,0);step(&i,1,0,0,7,1);unsigned a[]={1,3,2,4};CHECK(order(a,4));CHECK(!d.reorder_flows[0]&&!d.reorder_flows[1]&&!invariant_errors&&!warnings&&!freed);puts("independent flows: exact deletion order preserved");}
 return 0;
}
"""
needle="brcmu_pkt_buf_free_skb(rfi->pktslots[cur_idx]);"
assert funcs.count(needle)==2
with tempfile.TemporaryDirectory() as d:
 source=pathlib.Path(d)/"drivers/net/wireless/broadcom/brcm80211/brcmfmac/fwsignal.c"
 source.parent.mkdir(parents=True);source.write_bytes(raw)
 patch=pathlib.Path(__file__).resolve().parents[2]/"patches/brcmfmac-v6.18.50-rxreorder-count.patch"
 subprocess.run(["patch","--silent","-p1","-i",str(patch),"-d",d],check=True)
 changed=source.read_text()
 replacement="\t\t\t} else {\n\t\t\t\trfi->pktslots[cur_idx] = pkt;\n\t\t\t\trfi->pend_pkts++;"
 guard="\t\t\t} else {\n\t\t\t\tif (rfi->pktslots[cur_idx] != NULL) {\n\t\t\t\t\tbrcmu_pkt_buf_free_skb(rfi->pktslots[cur_idx]);\n\t\t\t\t\trfi->pend_pkts--;\n\t\t\t\t\trfi->pktslots[cur_idx] = NULL;\n\t\t\t\t}\n\t\t\t\trfi->pktslots[cur_idx] = pkt;\n\t\t\t\trfi->pend_pkts++;"
 assert s.count(replacement)==1
 expected=s.replace(needle,needle+"\n\t\t\t\trfi->pend_pkts--;").replace(replacement,guard)
 assert changed==expected
 candidate=changed[changed.index("static void brcmf_rxreorder_get_skb_list("):changed.index("void brcmf_fws_hdrpull(")]
 assert candidate==expected[expected.index("static void brcmf_rxreorder_get_skb_list("):expected.index("void brcmf_fws_hdrpull(")]
 for label,body in [("base",funcs),("candidate",candidate)]:
  p=pathlib.Path(d)/(label+".c");p.write_text(stub+defs+body+extended)
  exe=str(p.with_suffix(""));subprocess.run(["cc","-std=c11","-Wall","-Wextra","-Werror",str(p),"-o",exe],check=True)
  subprocess.run([exe]+(["fixed"]if label=="candidate"else[]),check=True)
 print("actual functions: eleven ordered fixtures and320 replacements verified; all three occupied-slot writes retain count/free invariants")
