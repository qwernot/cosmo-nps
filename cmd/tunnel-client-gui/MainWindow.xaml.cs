using System;
using System.Collections.Generic;
using System.Linq;
using System.Net.Http;
using System.Text;
using System.Text.Json;
using System.Text.Json.Serialization;
using System.Threading.Tasks;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Input;
using System.Windows.Media;
using System.Windows.Threading;

namespace TunnelClientGui
{
    public class Tunnel
    {
        [JsonPropertyName("id")]
        public string Id { get; set; } = "";

        [JsonPropertyName("userName")]
        public string UserName { get; set; } = "";

        [JsonPropertyName("nodeId")]
        public string NodeId { get; set; } = "";

        [JsonPropertyName("engine")]
        public string Engine { get; set; } = "";

        [JsonPropertyName("mode")]
        public string Mode { get; set; } = "";

        [JsonPropertyName("remotePort")]
        public int RemotePort { get; set; }

        [JsonPropertyName("localIp")]
        public string LocalIp { get; set; } = "";

        [JsonPropertyName("localPort")]
        public int LocalPort { get; set; }

        [JsonPropertyName("domains")]
        public List<string> Domains { get; set; } = new();

        [JsonPropertyName("remark")]
        public string Remark { get; set; } = "";

        [JsonPropertyName("enabled")]
        public bool Enabled { get; set; }

        // WPF Display Helpers
        public string DisplayType => $"{Engine?.ToUpper()}-{Mode?.ToUpper()}";
        public string DisplayLocal => $"{LocalIp}:{LocalPort}";
        public string DisplayRemote => (Mode?.ToLower() == "http" || Mode?.ToLower() == "https") && Domains != null && Domains.Any()
            ? string.Join(", ", Domains)
            : RemotePort > 0 ? RemotePort.ToString() : "--";
        public string DisplayStatus => Enabled ? "运行中" : "已禁用";
    }

    public class LauncherConfig
    {
        [JsonPropertyName("controlUrl")]
        public string ControlUrl { get; set; } = "";

        [JsonPropertyName("user")]
        public string User { get; set; } = "";

        [JsonPropertyName("password")]
        public string Password { get; set; } = "";

        [JsonPropertyName("refresh")]
        public string Refresh { get; set; } = "";
    }

    public class StatusResponse
    {
        [JsonPropertyName("config")]
        public LauncherConfig Config { get; set; } = new();

        [JsonPropertyName("running")]
        public bool Running { get; set; }

        [JsonPropertyName("started")]
        public DateTime Started { get; set; }

        [JsonPropertyName("tunnels")]
        public List<Tunnel> Tunnels { get; set; } = new();

        [JsonPropertyName("logs")]
        public List<string> Logs { get; set; } = new();
    }

    public class SettingsResponse
    {
        [JsonPropertyName("autoStart")]
        public bool AutoStart { get; set; }
    }

    public partial class MainWindow : Window
    {
        private readonly HttpClient _httpClient;
        private readonly string _baseUrl;
        private bool _isPolling;
        private DispatcherTimer? _uptimeTimer;
        private DateTime? _startedTime;
        private int _clearedLogCount;
        private int _lastLogsCount;
        private bool _hasLoadedConfig;

        public MainWindow(string baseUrl)
        {
            InitializeComponent();
            
            _baseUrl = baseUrl;
            _httpClient = new HttpClient { Timeout = TimeSpan.FromSeconds(5) };
            _isPolling = true;
            _clearedLogCount = 0;
            _lastLogsCount = 0;
            _hasLoadedConfig = false;

            DaemonAddressText.Text = _baseUrl;

            // Start polling loop
            _ = PollStatusAsync();

            // Uptime clock tick timer
            _uptimeTimer = new DispatcherTimer();
            _uptimeTimer.Interval = TimeSpan.FromSeconds(1);
            _uptimeTimer.Tick += UptimeTimer_Tick;
            _uptimeTimer.Start();
        }

        private async Task PollStatusAsync()
        {
            while (_isPolling)
            {
                try
                {
                    var response = await _httpClient.GetAsync($"{_baseUrl}/api/status");
                    if (response.IsSuccessStatusCode)
                    {
                        var json = await response.Content.ReadAsStringAsync();
                        var status = JsonSerializer.Deserialize<StatusResponse>(json);
                        if (status != null)
                        {
                            Dispatcher.Invoke(() => UpdateUi(status));
                        }
                    }
                    else
                    {
                        Dispatcher.Invoke(SetDisconnectedState);
                    }
                }
                catch
                {
                    Dispatcher.Invoke(SetDisconnectedState);
                }

                // Separately poll settings
                try
                {
                    var settingsResponse = await _httpClient.GetAsync($"{_baseUrl}/api/settings");
                    if (settingsResponse.IsSuccessStatusCode)
                    {
                        var json = await settingsResponse.Content.ReadAsStringAsync();
                        var settings = JsonSerializer.Deserialize<SettingsResponse>(json);
                        if (settings != null)
                        {
                            Dispatcher.Invoke(() =>
                            {
                                // Remove event handler temporarily to avoid feedback loop
                                AutoStartToggle.Click -= AutoStartToggle_Click;
                                AutoStartToggle.IsChecked = settings.AutoStart;
                                AutoStartToggle.Click += AutoStartToggle_Click;
                            });
                        }
                    }
                }
                catch
                {
                    // Ignore transient settings poll failures
                }

                await Task.Delay(1500);
            }
        }

        private void UpdateUi(StatusResponse status)
        {
            // Populate form fields on initial load from daemon config
            if (!_hasLoadedConfig && status.Config != null)
            {
                if (!string.IsNullOrEmpty(status.Config.User))
                {
                    UsernameBox.Text = status.Config.User;
                }
                if (!string.IsNullOrEmpty(status.Config.ControlUrl))
                {
                    ControlUrlBox.Text = status.Config.ControlUrl;
                }
                if (!string.IsNullOrEmpty(status.Config.Refresh))
                {
                    RefreshBox.Text = status.Config.Refresh;
                }
                _hasLoadedConfig = true;
            }

            // Update status badge & controls state
            if (status.Running)
            {
                StatusDot.Fill = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#10B981"));
                StatusText.Text = "运行中";
                StatusText.Foreground = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#10B981"));

                ConnectButton.IsEnabled = false;
                ConnectButton.Content = "开始连接";
                DisconnectButton.IsEnabled = true;

                ControlUrlBox.IsEnabled = false;
                UsernameBox.IsEnabled = false;
                PasswordBox.IsEnabled = false;
                RefreshBox.IsEnabled = false;

                _startedTime = status.Started;
                UpdateUptime();
            }
            else
            {
                StatusDot.Fill = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#F43F5E"));
                StatusText.Text = "已停止";
                StatusText.Foreground = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#64748B"));

                ConnectButton.IsEnabled = true;
                ConnectButton.Content = "开始连接";
                DisconnectButton.IsEnabled = false;

                ControlUrlBox.IsEnabled = true;
                UsernameBox.IsEnabled = true;
                PasswordBox.IsEnabled = true;
                RefreshBox.IsEnabled = true;

                _startedTime = null;
                UptimeText.Text = "-- : -- : --";
            }

            // Render tunnels
            TunnelsListView.ItemsSource = status.Tunnels;
            TunnelCountText.Text = $"{status.Tunnels?.Count ?? 0} 个";
            ClientUserText.Text = string.IsNullOrEmpty(status.Config?.User) ? "未载入" : status.Config.User;

            // Handle logs with client-side clear filter
            if (status.Logs != null)
            {
                _lastLogsCount = status.Logs.Count;
                var displayLogs = status.Logs;

                if (_clearedLogCount > 0)
                {
                    if (status.Logs.Count >= _clearedLogCount)
                    {
                        displayLogs = status.Logs.Skip(_clearedLogCount).ToList();
                    }
                    else
                    {
                        _clearedLogCount = 0; // Reset if daemon's log array got cleared/truncated
                    }
                }

                bool shouldScroll = LogTextBox.SelectionLength == 0 && (LogTextBox.CaretIndex >= LogTextBox.Text.Length - 10 || !LogTextBox.IsFocused);
                LogTextBox.Text = string.Join(Environment.NewLine, displayLogs);
                if (shouldScroll)
                {
                    LogTextBox.ScrollToEnd();
                }
            }
        }

        private void SetDisconnectedState()
        {
            StatusDot.Fill = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#F43F5E"));
            StatusText.Text = "已断开 (未连接服务)";
            StatusText.Foreground = new SolidColorBrush((Color)ColorConverter.ConvertFromString("#F43F5E"));

            ConnectButton.IsEnabled = false;
            DisconnectButton.IsEnabled = false;

            ControlUrlBox.IsEnabled = true;
            UsernameBox.IsEnabled = true;
            PasswordBox.IsEnabled = true;
            RefreshBox.IsEnabled = true;

            _startedTime = null;
            UptimeText.Text = "-- : -- : --";
            TunnelCountText.Text = "0 个";
            ClientUserText.Text = "未载入";
        }

        private void UptimeTimer_Tick(object? sender, EventArgs e)
        {
            UpdateUptime();
        }

        private void UpdateUptime()
        {
            if (_startedTime.HasValue)
            {
                var elapsed = DateTime.UtcNow - _startedTime.Value.ToUniversalTime();
                if (elapsed.Ticks < 0) elapsed = TimeSpan.Zero;
                UptimeText.Text = string.Format("{0:00} : {1:00} : {2:00}", (int)elapsed.TotalHours, elapsed.Minutes, elapsed.Seconds);
            }
            else
            {
                UptimeText.Text = "-- : -- : --";
            }
        }

        private void NavButton_Checked(object sender, RoutedEventArgs e)
        {
            if (sender is RadioButton rb && MainTabControl != null)
            {
                int index = int.Parse(rb.Tag?.ToString() ?? "0");
                MainTabControl.SelectedIndex = index;
            }
        }

        private void TitleBar_MouseLeftButtonDown(object sender, MouseButtonEventArgs e)
        {
            if (e.ButtonState == MouseButtonState.Pressed)
            {
                DragMove();
            }
        }

        private void MinimizeButton_Click(object sender, RoutedEventArgs e)
        {
            WindowState = WindowState.Minimized;
        }

        private void CloseButton_Click(object sender, RoutedEventArgs e)
        {
            _isPolling = false;
            _uptimeTimer?.Stop();
            Close();
        }

        private async void ConnectButton_Click(object sender, RoutedEventArgs e)
        {
            string server = ControlUrlBox.Text.Trim();
            string user = UsernameBox.Text.Trim();
            string password = PasswordBox.Password;
            string refresh = RefreshBox.Text.Trim();

            if (string.IsNullOrEmpty(server) || string.IsNullOrEmpty(user) || string.IsNullOrEmpty(password))
            {
                MessageBox.Show("请填写完整的服务器地址、用户名及密码。", "提示", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            ConnectButton.IsEnabled = false;
            ConnectButton.Content = "正在连接...";

            try
            {
                var body = new
                {
                    controlUrl = server,
                    user = user,
                    password = password,
                    refresh = refresh
                };
                var content = new StringContent(JsonSerializer.Serialize(body), Encoding.UTF8, "application/json");
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/start", content);
                if (response.IsSuccessStatusCode)
                {
                    // UI will auto-refresh on next status poll
                }
                else
                {
                    var errJson = await response.Content.ReadAsStringAsync();
                    string errMsg = "未知错误";
                    try
                    {
                        var errObj = JsonSerializer.Deserialize<Dictionary<string, string>>(errJson);
                        if (errObj != null && errObj.TryGetValue("error", out var msg))
                        {
                            errMsg = msg;
                        }
                    }
                    catch { }
                    MessageBox.Show($"连接失败: {errMsg}", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                    ConnectButton.IsEnabled = true;
                    ConnectButton.Content = "开始连接";
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show($"网络请求失败: {ex.Message}", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                ConnectButton.IsEnabled = true;
                ConnectButton.Content = "开始连接";
            }
        }

        private async void DisconnectButton_Click(object sender, RoutedEventArgs e)
        {
            DisconnectButton.IsEnabled = false;
            DisconnectButton.Content = "正在断开...";

            try
            {
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/stop", null);
                if (response.IsSuccessStatusCode)
                {
                    // UI will auto-refresh on next status poll
                }
                else
                {
                    MessageBox.Show("停止服务请求失败", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                    DisconnectButton.IsEnabled = true;
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show($"停止服务异常: {ex.Message}", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                DisconnectButton.IsEnabled = true;
            }
            finally
            {
                DisconnectButton.Content = "断开连接";
            }
        }

        private void ClearLogsButton_Click(object sender, RoutedEventArgs e)
        {
            _clearedLogCount = _lastLogsCount;
            LogTextBox.Clear();
        }

        private async void AutoStartToggle_Click(object sender, RoutedEventArgs e)
        {
            bool isChecked = AutoStartToggle.IsChecked == true;
            try
            {
                var body = new { autoStart = isChecked };
                var content = new StringContent(JsonSerializer.Serialize(body), Encoding.UTF8, "application/json");
                var response = await _httpClient.PostAsync($"{_baseUrl}/api/settings", content);
                if (!response.IsSuccessStatusCode)
                {
                    MessageBox.Show("保存自启动设置失败", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                    AutoStartToggle.IsChecked = !isChecked;
                }
            }
            catch (Exception ex)
            {
                MessageBox.Show($"保存自启动设置异常: {ex.Message}", "错误", MessageBoxButton.OK, MessageBoxImage.Error);
                AutoStartToggle.IsChecked = !isChecked;
            }
        }
    }
}