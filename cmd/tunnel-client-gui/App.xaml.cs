using System;
using System.Runtime.InteropServices;
using System.Threading;
using System.Windows;

namespace TunnelClientGui
{
    public partial class App : Application
    {
        private const string MutexName = "TunnelClientGui_SingleInstance_Mutex";
        private static Mutex? _mutex;

        [DllImport("user32.dll")]
        private static extern bool SetForegroundWindow(IntPtr hWnd);

        [DllImport("user32.dll")]
        private static extern bool ShowWindow(IntPtr hWnd, int nCmdShow);

        [DllImport("user32.dll")]
        private static extern IntPtr FindWindow(string? lpClassName, string lpWindowName);

        private const int SW_RESTORE = 9;
        private const int SW_SHOW = 5;

        protected override void OnStartup(StartupEventArgs e)
        {
            _mutex = new Mutex(true, MutexName, out bool isNew);
            if (!isNew)
            {
                // Another GUI instance is already running — activate its window
                var hwnd = FindWindow(null, "Tunnel Client");
                if (hwnd != IntPtr.Zero)
                {
                    ShowWindow(hwnd, SW_RESTORE);
                    ShowWindow(hwnd, SW_SHOW);
                    SetForegroundWindow(hwnd);
                }
                Shutdown();
                return;
            }

            // Parse daemon API base URL from command-line args
            string baseUrl = "http://127.0.0.1:18090";
            if (e.Args.Length > 0 && !string.IsNullOrWhiteSpace(e.Args[0]))
            {
                baseUrl = e.Args[0].TrimEnd('/');
            }

            var mainWindow = new MainWindow(baseUrl);
            mainWindow.Show();
        }
    }
}
