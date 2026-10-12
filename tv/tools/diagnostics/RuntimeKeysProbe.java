import android.os.Debug;
import java.util.Map;
public class RuntimeKeysProbe {
 public static void main(String[] args) {
  System.out.println("waitingKey="+Debug.getRuntimeStat("art.gc.total-time-waiting-for-gc"));
  for(Map.Entry<String,String> entry:Debug.getRuntimeStats().entrySet())
   System.out.println(entry.getKey()+"="+entry.getValue());
 }
}
