/* Tiny ASCII error captions. This touches only newly allocated overlay pixels. */
static const guint8 caption_letters[26][7]={
 {14,17,17,31,17,17,17},{30,17,17,30,17,17,30},{14,17,16,16,16,17,14},{30,17,17,17,17,17,30},{31,16,16,30,16,16,31},{31,16,16,30,16,16,16},{14,17,16,23,17,17,15},{17,17,17,31,17,17,17},{14,4,4,4,4,4,14},{7,2,2,2,18,18,12},{17,18,20,24,20,18,17},{16,16,16,16,16,16,31},{17,27,21,21,17,17,17},{17,25,25,21,19,19,17},{14,17,17,17,17,17,14},{30,17,17,30,16,16,16},{14,17,17,17,21,18,13},{30,17,17,30,20,18,17},{15,16,16,14,1,1,30},{31,4,4,4,4,4,4},{17,17,17,17,17,17,14},{17,17,17,17,17,10,4},{17,17,17,21,21,27,17},{17,17,10,4,10,17,17},{17,17,10,4,4,4,4},{31,1,2,4,8,16,31}};
static void caption_pixels(guint8 *pixels,guint width,guint height,const gchar *text){
 guint len=MIN((guint)strlen(text),(width>20?width-20:0)/6),scale=MIN(2u,(width>20?width-20:0)/MAX(1u,len*6));if(!len||!scale)return;
 /* Opaque black only while showing an error; recovered overlays are transparent. */
 for(guint n=0;n<width*height;n++)pixels[n*4+3]=255;
 guint top=height>7*scale?(height-7*scale)/2:0;
 for(guint i=0;i<len;i++){gchar c=g_ascii_toupper(text[i]);for(guint row=0;row<7;row++){guint8 bits=c>='A'&&c<='Z'?caption_letters[c-'A'][row]:(c=='-'&&row==3?31:0);for(guint col=0;col<5;col++)if(bits&(1u<<(4-col)))for(guint y=0;y<scale;y++)for(guint x=0;x<scale;x++){guint px=8+(i*6+col)*scale+x,py=top+row*scale+y;if(px<width&&py<height){guint at=(py*width+px)*4;pixels[at]=pixels[at+1]=pixels[at+2]=255;}}}}
}
