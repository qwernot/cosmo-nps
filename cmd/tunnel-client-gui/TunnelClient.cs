using System;
using System.Diagnostics;
using System.Drawing;
using System.IO;
using System.Reflection;
using System.Text;
using System.Windows.Forms;

namespace TunnelControl.ClientGui
{
    internal static class Program
    {
        [STAThread]
        private static void Main()
        {
            Application.EnableVisualStyles();
            Application.SetCompatibleTextRenderingDefault(false);
            Application.Run(new MainForm());
        }
    }

    internal sealed class MainForm : Form
    {
        private readonly TextBox serverBox = new TextBox();
        private readonly TextBox userBox = new TextBox();
        private readonly TextBox passwordBox = new TextBox();
        private readonly TextBox refreshBox = new TextBox();
        private readonly RichTextBox logBox = new RichTextBox();
        private readonly Label statusLabel = new Label();
        private readonly Button connectButton = new Button();
        private readonly Button stopButton = new Button();
        private Process clientProcess;

        private static readonly Color Bg = Color.FromArgb(13, 22, 35);
        private static readonly Color Sidebar = Color.FromArgb(8, 13, 22);
        private static readonly Color Card = Color.FromArgb(24, 36, 54);
        private static readonly Color Input = Color.FromArgb(15, 23, 42);
        private static readonly Color Muted = Color.FromArgb(148, 163, 184);
        private static readonly Color TextMain = Color.FromArgb(241, 245, 249);
        private static readonly Color Accent = Color.FromArgb(37, 99, 235);
        private static readonly Color Danger = Color.FromArgb(220, 38, 38);

        public MainForm()
        {
            Text = "Tunnel Client";
            StartPosition = FormStartPosition.Manual;
            Location = new Point(80, 80);
            AutoScaleMode = AutoScaleMode.None;
            MinimumSize = new Size(640, 500);
            Size = new Size(680, 520);
            BackColor = Bg;
            Font = new Font("Segoe UI", 9F);
            BuildLayout();
            LoadConfig();
        }

        private void BuildLayout()
        {
            Controls.Add(BuildMain());
            Controls.Add(BuildSidebar());
        }

        private Control BuildSidebar()
        {
            var side = new Panel { Dock = DockStyle.Left, Width = 128, BackColor = Sidebar, Padding = new Padding(10, 14, 8, 12) };
            var stack = new FlowLayoutPanel { Dock = DockStyle.Fill, FlowDirection = FlowDirection.TopDown, WrapContents = false, AutoScroll = false, BackColor = Sidebar };
            side.Controls.Add(stack);

            stack.Controls.Add(new Label { Text = "Tunnel", ForeColor = Color.White, Font = new Font("Segoe UI Semibold", 13F, FontStyle.Bold), Width = 105, Height = 28 });
            stack.Controls.Add(new Label { Text = "NPS Client", ForeColor = Color.FromArgb(125, 211, 252), Width = 105, Height = 22 });
            stack.Controls.Add(Spacer(12));
            stack.Controls.Add(NavItem("Client", true));
            stack.Controls.Add(NavItem("Logs", false));
            stack.Controls.Add(NavItem("Settings", false));
            stack.Controls.Add(Spacer(86));
            stack.Controls.Add(new Label { Text = "Tunnel Control", ForeColor = Muted, Width = 105, Height = 24 });
            return side;
        }

        private Control BuildMain()
        {
            var root = new TableLayoutPanel { Dock = DockStyle.Fill, Padding = new Padding(12), BackColor = Bg, ColumnCount = 1, RowCount = 4 };
            root.RowStyles.Add(new RowStyle(SizeType.Absolute, 58));
            root.RowStyles.Add(new RowStyle(SizeType.Absolute, 82));
            root.RowStyles.Add(new RowStyle(SizeType.Absolute, 180));
            root.RowStyles.Add(new RowStyle(SizeType.Percent, 100));

            var header = new Panel { Dock = DockStyle.Fill, BackColor = Bg };
            header.Controls.Add(new Label { Text = "Cloud Tunnel Client", ForeColor = TextMain, Font = new Font("Segoe UI Semibold", 11F, FontStyle.Bold), Dock = DockStyle.Top, Height = 24 });
            header.Controls.Add(new Label { Text = "Start local NPS client with your account.", ForeColor = Muted, Dock = DockStyle.Bottom, Height = 22 });
            root.Controls.Add(header, 0, 0);

            var status = CardPanel();
            status.Padding = new Padding(16, 11, 16, 10);
            statusLabel.Text = "Disconnected";
            statusLabel.ForeColor = TextMain;
            statusLabel.Font = new Font("Segoe UI Semibold", 12F, FontStyle.Bold);
            statusLabel.Dock = DockStyle.Top;
            statusLabel.Height = 26;
            status.Controls.Add(statusLabel);
            status.Controls.Add(new Label { Text = "Status updates from client core logs.", ForeColor = Muted, Dock = DockStyle.Bottom, Height = 22 });
            root.Controls.Add(status, 0, 1);

            root.Controls.Add(BuildConfigCard(), 0, 2);
            root.Controls.Add(BuildLogCard(), 0, 3);
            return root;
        }

        private Control BuildConfigCard()
        {
            var card = CardPanel();
            card.Padding = new Padding(14);

            var grid = new TableLayoutPanel { Dock = DockStyle.Fill, ColumnCount = 4, RowCount = 4, BackColor = Card };
            grid.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 54));
            grid.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 50));
            grid.ColumnStyles.Add(new ColumnStyle(SizeType.Absolute, 64));
            grid.ColumnStyles.Add(new ColumnStyle(SizeType.Percent, 50));
            grid.RowStyles.Add(new RowStyle(SizeType.Absolute, 30));
            grid.RowStyles.Add(new RowStyle(SizeType.Absolute, 38));
            grid.RowStyles.Add(new RowStyle(SizeType.Absolute, 38));
            grid.RowStyles.Add(new RowStyle(SizeType.Absolute, 38));
            card.Controls.Add(grid);

            var title = new Label { Text = "Connection", ForeColor = TextMain, Font = new Font("Segoe UI Semibold", 10F, FontStyle.Bold), Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft };
            grid.Controls.Add(title, 0, 0);
            grid.SetColumnSpan(title, 4);

            AddInputRow(grid, 1, "Server", serverBox, "http://192.168.6.64:8088", false, 0, 3);
            AddInputRow(grid, 2, "User", userBox, "", false, 0, 1);
            AddInputRow(grid, 2, "Password", passwordBox, "", true, 2, 1);
            AddInputRow(grid, 3, "Refresh", refreshBox, "30s", false, 0, 1);

            var buttons = new FlowLayoutPanel { Dock = DockStyle.Fill, FlowDirection = FlowDirection.LeftToRight, BackColor = Card, WrapContents = false };
            connectButton.Text = "Connect";
            StyleButton(connectButton, Accent);
            connectButton.Click += delegate { StartClient(); };
            stopButton.Text = "Stop";
            StyleButton(stopButton, Danger);
            stopButton.Click += delegate { StopClient(); };
            buttons.Controls.Add(connectButton);
            buttons.Controls.Add(stopButton);
            grid.Controls.Add(buttons, 2, 3);
            grid.SetColumnSpan(buttons, 2);
            return card;
        }

        private Control BuildLogCard()
        {
            var card = CardPanel();
            card.Padding = new Padding(14);
            var title = new Label { Text = "Runtime Logs", ForeColor = TextMain, Font = new Font("Segoe UI Semibold", 10F, FontStyle.Bold), Dock = DockStyle.Top, Height = 26 };
            card.Controls.Add(title);
            logBox.Dock = DockStyle.Fill;
            logBox.BorderStyle = BorderStyle.None;
            logBox.BackColor = Color.FromArgb(7, 12, 22);
            logBox.ForeColor = Color.FromArgb(191, 219, 254);
            logBox.Font = new Font("Consolas", 8.5F);
            logBox.ReadOnly = true;
            logBox.Text = "Waiting for connection...\n";
            card.Controls.Add(logBox);
            logBox.BringToFront();
            return card;
        }

        private void AddInputRow(TableLayoutPanel grid, int row, string label, TextBox box, string placeholder, bool password, int col, int span)
        {
            grid.Controls.Add(new Label { Text = label, ForeColor = Color.FromArgb(203, 213, 225), Dock = DockStyle.Fill, TextAlign = ContentAlignment.MiddleLeft }, col, row);
            StyleInput(box, placeholder, password);
            grid.Controls.Add(box, col + 1, row);
            if (span > 1)
            {
                grid.SetColumnSpan(box, span);
            }
        }

        private static Panel CardPanel()
        {
            return new Panel { Dock = DockStyle.Fill, Margin = new Padding(0, 0, 0, 18), BackColor = Card };
        }

        private static Control NavItem(string text, bool active)
        {
            return new Label
            {
                Text = text,
                Width = 105,
                Height = 34,
                Margin = new Padding(0, 0, 0, 10),
                Padding = new Padding(16, 0, 0, 0),
                TextAlign = ContentAlignment.MiddleLeft,
                BackColor = active ? Accent : Sidebar,
                ForeColor = active ? Color.White : Muted,
                Font = new Font("Segoe UI Semibold", 9F, FontStyle.Bold)
            };
        }

        private static Control Spacer(int height)
        {
            return new Label { Width = 105, Height = height };
        }

        private static void StyleInput(TextBox box, string placeholder, bool password)
        {
            box.Dock = DockStyle.Fill;
            box.Margin = new Padding(0, 4, 10, 4);
            box.BorderStyle = BorderStyle.FixedSingle;
            box.BackColor = Input;
            box.ForeColor = Color.White;
            box.Text = placeholder;
            box.PasswordChar = password ? '*' : '\0';
        }

        private static void StyleButton(Button button, Color color)
        {
            button.Width = 68;
            button.Height = 28;
            button.Margin = new Padding(0, 4, 8, 4);
            button.FlatStyle = FlatStyle.Flat;
            button.FlatAppearance.BorderSize = 0;
            button.BackColor = color;
            button.ForeColor = Color.White;
            button.Font = new Font("Segoe UI Semibold", 9F, FontStyle.Bold);
        }

        private void StartClient()
        {
            if (clientProcess != null && !clientProcess.HasExited)
            {
                AppendLog("Client is already running.");
                return;
            }
            var core = EnsureCoreBinary();
            if (!File.Exists(core))
            {
                AppendLog("Cannot prepare embedded client core.");
                return;
            }
            if (string.IsNullOrWhiteSpace(serverBox.Text) || string.IsNullOrWhiteSpace(userBox.Text) || string.IsNullOrWhiteSpace(passwordBox.Text))
            {
                AppendLog("Server, user and password are required.");
                return;
            }
            SaveConfig();
            var args = string.Format("-no-gui -server \"{0}\" -user \"{1}\" -password \"{2}\" -refresh \"{3}\"", Escape(serverBox.Text), Escape(userBox.Text), Escape(passwordBox.Text), Escape(refreshBox.Text));
            clientProcess = new Process
            {
                StartInfo = new ProcessStartInfo
                {
                    FileName = core,
                    Arguments = args,
                    UseShellExecute = false,
                    RedirectStandardOutput = true,
                    RedirectStandardError = true,
                    CreateNoWindow = true,
                    StandardOutputEncoding = Encoding.UTF8,
                    StandardErrorEncoding = Encoding.UTF8
                },
                EnableRaisingEvents = true
            };
            clientProcess.OutputDataReceived += delegate(object sender, DataReceivedEventArgs e) { OnCoreLog(e.Data); };
            clientProcess.ErrorDataReceived += delegate(object sender, DataReceivedEventArgs e) { OnCoreLog(e.Data); };
            clientProcess.Exited += delegate
            {
                BeginInvoke(new Action(delegate
                {
                    statusLabel.Text = "Disconnected";
                    AppendLog("Client exited.");
                }));
            };
            clientProcess.Start();
            clientProcess.BeginOutputReadLine();
            clientProcess.BeginErrorReadLine();
            statusLabel.Text = "Connecting";
            AppendLog("Connecting to " + serverBox.Text);
        }

        private void StopClient()
        {
            if (clientProcess == null || clientProcess.HasExited)
            {
                return;
            }
            AppendLog("Stopping client.");
            clientProcess.Kill();
        }

        private void OnCoreLog(string line)
        {
            if (string.IsNullOrWhiteSpace(line))
            {
                return;
            }
            BeginInvoke(new Action(delegate
            {
                if (line.IndexOf("logged in", StringComparison.OrdinalIgnoreCase) >= 0 || line.IndexOf("Successful connection", StringComparison.OrdinalIgnoreCase) >= 0)
                {
                    statusLabel.Text = "Connected";
                }
                AppendLog(line);
            }));
        }

        private void AppendLog(string line)
        {
            logBox.AppendText(DateTime.Now.ToString("HH:mm:ss") + "  " + line + Environment.NewLine);
            logBox.SelectionStart = logBox.TextLength;
            logBox.ScrollToCaret();
        }

        private void LoadConfig()
        {
            var path = ConfigPath();
            if (!File.Exists(path))
            {
                return;
            }
            foreach (var line in File.ReadAllLines(path))
            {
                var parts = line.Split(new[] { '=' }, 2);
                if (parts.Length != 2)
                {
                    continue;
                }
                if (parts[0] == "server") serverBox.Text = parts[1];
                if (parts[0] == "user") userBox.Text = parts[1];
                if (parts[0] == "refresh") refreshBox.Text = parts[1];
            }
        }

        private void SaveConfig()
        {
            var dir = Path.GetDirectoryName(ConfigPath());
            if (!Directory.Exists(dir))
            {
                Directory.CreateDirectory(dir);
            }
            File.WriteAllLines(ConfigPath(), new[] { "server=" + serverBox.Text, "user=" + userBox.Text, "refresh=" + refreshBox.Text });
        }

        private static string ConfigPath()
        {
            return Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.ApplicationData), "TunnelControl", "client-gui.ini");
        }

        private static string Escape(string value)
        {
            return value.Replace("\"", "\\\"");
        }

        private static string EnsureCoreBinary()
        {
            var dir = Path.Combine(Environment.GetFolderPath(Environment.SpecialFolder.LocalApplicationData), "TunnelControl", "Client");
            Directory.CreateDirectory(dir);
            var target = Path.Combine(dir, "tunnel-client-core.exe");
            var asm = Assembly.GetExecutingAssembly();
            using (var input = asm.GetManifestResourceStream("TunnelControl.ClientGui.tunnel-client-core.exe"))
            {
                if (input == null)
                {
                    return target;
                }
                using (var output = File.Create(target))
                {
                    input.CopyTo(output);
                }
            }
            return target;
        }
    }
}
