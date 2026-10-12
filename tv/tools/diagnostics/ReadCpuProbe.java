import android.os.Debug;
import android.os.Process;
import java.net.ServerSocket;
import java.net.Socket;
import java.io.InputStream;
import java.util.Arrays;
import java.util.concurrent.CountDownLatch;

public class ReadCpuProbe {
    static volatile long sink;
    static long[] cpuBench() {
        for(int i=0;i<1000;i++) sink=Debug.threadCpuTimeNanos();
        long[] values=new long[200];
        for(int b=0;b<values.length;b++) {
            long start=System.nanoTime();
            for(int i=0;i<100;i++) sink=Debug.threadCpuTimeNanos();
            values[b]=(System.nanoTime()-start)/100;
        }
        Arrays.sort(values);return values;
    }
    static long[] gcBench(String key) {
        long[] values=new long[100];
        for(int b=0;b<values.length;b++) {
            long start=System.nanoTime();
            for(int i=0;i<10;i++) {String v=Debug.getRuntimeStat(key);sink=v==null?-1:v.length();}
            values[b]=(System.nanoTime()-start)/10;
        }
        Arrays.sort(values);return values;
    }
    static String percentile(long[] v) {return "{\"medianNs\":"+v[v.length/2]+",\"p95Ns\":"+v[(v.length-1)*95/100]+",\"maxBatchMeanNs\":"+v[v.length-1]+"}";}
    static String span(String kind,long mono0,long cpu0,int tid0) {
        long cpu1=Debug.threadCpuTimeNanos();long mono1=System.nanoTime();int tid1=Process.myTid();
        return "{\"kind\":\""+kind+"\",\"tidBefore\":"+tid0+",\"tidAfter\":"+tid1+",\"elapsedNs\":"+(mono1-mono0)+",\"threadCpuDeltaNs\":"+((cpu0<0||cpu1<cpu0)?-1:cpu1-cpu0)+"}";
    }
    public static void main(String[] args)throws Exception {
        System.out.println("{\"counterSupportValue\":"+Debug.threadCpuTimeNanos()+",\"cpuCounterCost\":"+percentile(cpuBench())+"}");
        for(String key:new String[]{"art.gc.gc-count","art.gc.gc-time","art.gc.blocking-gc-count","art.gc.blocking-gc-time"})
            System.out.println("{\"runtimeKey\":\""+key+"\",\"value\":\""+Debug.getRuntimeStat(key)+"\",\"lookupCost\":"+percentile(gcBench(key))+"}");
        int tid=Process.myTid();long cpu=Debug.threadCpuTimeNanos();long mono=System.nanoTime();Thread.sleep(30);System.out.println(span("sleep30ms",mono,cpu,tid));
        ServerSocket server=new ServerSocket(0);CountDownLatch ready=new CountDownLatch(1);
        Thread writer=new Thread(()->{try {Socket peer=server.accept();ready.countDown();Thread.sleep(40);peer.getOutputStream().write(1);peer.close();}catch(Exception e){throw new RuntimeException(e);}});writer.start();
        Socket socket=new Socket("127.0.0.1",server.getLocalPort());InputStream input=socket.getInputStream();ready.await();
        tid=Process.myTid();cpu=Debug.threadCpuTimeNanos();mono=System.nanoTime();if(input.read()!=1)throw new IllegalStateException("bad byte");System.out.println(span("localSocketRead40ms",mono,cpu,tid));socket.close();server.close();writer.join();
        tid=Process.myTid();cpu=Debug.threadCpuTimeNanos();mono=System.nanoTime();long stop=mono+20000000L;long value=1;
        while(System.nanoTime()<stop) value=value*1664525L+1013904223L;
        sink=value;System.out.println(span("busy20ms",mono,cpu,tid));
    }
}
