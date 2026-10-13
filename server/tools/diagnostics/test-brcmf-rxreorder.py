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
struct sk_buff {u8 cb[32];struct sk_buff*next;};
struct brcmf_skb_reorder_data {u8 reorder[10];};
struct sk_buff_head {struct sk_buff*head,*tail;unsigned count;};
struct brcmf_ampdu_rx_reorder {u8 flow_id,cur_idx,exp_idx,max_idx,pend_pkts;struct sk_buff*pktslots[];};
struct brcmf_pub {struct brcmf_ampdu_rx_reorder*reorder_flows[256];};
struct brcmf_if {struct brcmf_pub*drvr;};
static unsigned warnings,delivered,freed;
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
static void brcmu_pkt_buf_free_skb(struct sk_buff*p){freed++;free(p);}
static void brcmf_netif_rx(struct brcmf_if*i,struct sk_buff*p){(void)i;delivered++;free(p);}
"""
test=r"""
static void send(struct brcmf_if*i,u8 flags,u8 cur,u8 exp){struct sk_buff*p=calloc(1,sizeof(*p));p->cb[2]=7;p->cb[4]=flags;p->cb[6]=cur;p->cb[8]=exp;brcmf_fws_rxreorder(i,p);}
int main(int argc,char**argv){(void)argv;unsigned expected=argc>1?0:1;
 for(unsigned scenario=0;scenario<2;scenario++){
  struct brcmf_pub d={0};struct brcmf_if i={&d};warnings=delivered=freed=0;
  send(&i,0x10,2,scenario?2:1);send(&i,0x04,2,scenario?3:1);
  send(&i,0x10,4,4);
  if(warnings!=expected||freed!=1||d.reorder_flows[0]->pend_pkts!=(expected?2:1))return 1;
  send(&i,0x01,0,0);if(delivered!=3||d.reorder_flows[0])return 2;
  printf("scenario=%u warnings=%u delivered=%u replaced=%u\n",scenario,warnings,delivered,freed);
 }
 struct brcmf_pub d={0};struct brcmf_if i={&d};warnings=delivered=freed=0;
 send(&i,0x10,2,1);send(&i,0x04,3,1);send(&i,0x10,4,4);
 if(warnings||freed||d.reorder_flows[0]->pend_pkts!=1)return 3;
 send(&i,0x01,0,0);if(delivered!=4)return 4;
 puts("nonduplicate control: warnings=0 delivered=4 replaced=0");
 struct brcmf_pub burst={0};struct brcmf_if j={&burst};warnings=delivered=freed=0;
 send(&j,0x10,2,1);for(unsigned k=0;k<16;k++)send(&j,0x04,2,1);
 send(&j,0x10,4,4);if(warnings!=expected||freed!=16||burst.reorder_flows[0]->pend_pkts!=(expected?17:1))return 5;
 send(&j,0x01,0,0);if(delivered!=3)return 6;
 printf("replacement burst: warnings=%u delivered=%u replaced=%u\n",warnings,delivered,freed);return 0;}
"""
needle="brcmu_pkt_buf_free_skb(rfi->pktslots[cur_idx]);"
assert funcs.count(needle)==2
candidate=funcs.replace(needle,needle+"\n\t\t\t\trfi->pend_pkts--;")
with tempfile.TemporaryDirectory() as d:
 for label,body in [("base",funcs),("candidate",candidate)]:
  p=pathlib.Path(d)/(label+".c");p.write_text(stub+defs+body+test)
  exe=str(p.with_suffix(""));subprocess.run(["cc","-std=c11","-Wall","-Wextra","-Werror",str(p),"-o",exe],check=True)
  subprocess.run([exe]+(["fixed"]if label=="candidate"else[]),check=True)
 print("actual source functions: baseline duplicate-slot accounting warnings reproduced; candidate accounting repair passes both branches")
