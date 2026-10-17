package jp.i32.chain;

import android.os.Bundle;
import com.getcapacitor.BridgeActivity;
import jp.i32.chain.haptics.ChainHapticsPlugin;
import jp.i32.chain.proximity.ChainProximityPlugin;

public class MainActivity extends BridgeActivity {
    @Override
    public void onCreate(Bundle savedInstanceState) {
        // アプリ内のプラグインは super.onCreate より前に登録する
        registerPlugin(ChainHapticsPlugin.class);
        registerPlugin(ChainProximityPlugin.class);
        super.onCreate(savedInstanceState);
    }
}
