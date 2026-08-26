param([int]$ProcId)
Add-Type @"
using System;
using System.Runtime.InteropServices;
using System.Text;
public class WinEnum {
  [DllImport("user32.dll")] public static extern bool EnumWindows(CB lpEnumFunc, IntPtr lParam);
  [DllImport("user32.dll")] public static extern bool GetWindowRect(IntPtr h, out RECT r);
  [DllImport("user32.dll")] public static extern int GetWindowTextW(IntPtr h, [MarshalAs(UnmanagedType.LPWStr)] StringBuilder s, int n);
  [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr h);
  [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr h, out uint pid);
  public struct RECT { public int L, T, R, B; }
  public delegate bool CB(IntPtr h, IntPtr lParam);
  public static void Dump(int procId) {
    EnumWindows(delegate(IntPtr h, IntPtr p) {
      uint pid; GetWindowThreadProcessId(h, out pid);
      if (pid == (uint)procId) {
        var sb = new StringBuilder(256); GetWindowTextW(h, sb, 256);
        RECT r; GetWindowRect(h, out r);
        Console.WriteLine("vis=" + IsWindowVisible(h) + " title='" + sb + "' rect=" + r.L + "," + r.T + " " + (r.R - r.L) + "x" + (r.B - r.T));
      }
      return true;
    }, IntPtr.Zero);
  }
}
"@
[WinEnum]::Dump($ProcId)
