// Decompile functions by name prefix to a file.
// postScript args: <prefix> <outputPath> [secondPrefix]
import ghidra.app.script.GhidraScript;
import ghidra.app.decompiler.DecompInterface;
import ghidra.app.decompiler.DecompileResults;
import ghidra.program.model.listing.Function;
import ghidra.program.model.listing.FunctionIterator;
import java.io.FileWriter;
import java.io.PrintWriter;

public class DecompileByPrefix extends GhidraScript {

    @Override
    public void run() throws Exception {
        String[] args = getScriptArgs();
        String prefix = args.length > 0 ? args[0] : "Java_com_axis_net_config_";
        String outfile = args.length > 1 ? args[1] : "/out/decomp.c";
        String second = args.length > 2 ? args[2] : null;

        DecompInterface di = new DecompInterface();
        di.openProgram(currentProgram);
        int hit = 0;
        PrintWriter pw = new PrintWriter(new FileWriter(outfile));
        FunctionIterator it = currentProgram.getFunctionManager().getFunctions(true);
        while (it.hasNext()) {
            Function f = it.next();
            String n = f.getName();
            boolean match = n.startsWith(prefix) || (second != null && n.startsWith(second));
            if (!match) {
                continue;
            }
            DecompileResults r = di.decompileFunction(f, 180, monitor);
            String c = (r != null && r.decompileCompleted() && r.getDecompiledFunction() != null)
                    ? r.getDecompiledFunction().getC() : "// DECOMPILE FAILED";
            pw.println("//===== " + n);
            pw.println(c);
            pw.println();
            hit++;
        }
        pw.close();
        println("AXIS_DECOMP_DONE hit=" + hit + " out=" + outfile);
    }
}
