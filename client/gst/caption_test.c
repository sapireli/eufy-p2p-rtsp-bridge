#include <assert.h>
#include <stdint.h>
#include <string.h>
#include <ctype.h>
#include <stdlib.h>
typedef unsigned int guint;
typedef uint8_t guint8;
typedef char gchar;
#define MIN(a,b) ((a)<(b)?(a):(b))
#define MAX(a,b) ((a)>(b)?(a):(b))
#define g_ascii_toupper(c) toupper((unsigned char)(c))
#include "caption.h"
int main(void){
 for(guint width=40;width<=640;width+=200){guint size=width*22*4;guint8 *pixels=calloc(1,size+16);memset(pixels+size,0x5a,16);caption_pixels(pixels,width,22,"");for(guint i=0;i<size;i++)assert(pixels[i]==0);caption_pixels(pixels,width,22,"DNS FAILED - CHECK BRIDGE ADDRESS");guint white=0;for(guint i=0;i<size;i+=4)if(pixels[i]==255)white++;assert(white>0);for(guint i=size;i<size+16;i++)assert(pixels[i]==0x5a);memset(pixels,0,size);caption_pixels(pixels,width,22,"");for(guint i=0;i<size;i++)assert(pixels[i]==0);free(pixels);}
 return 0;
}
