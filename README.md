# DeskTime Activity Tracker - Simplified Version

A streamlined Windows desktop application that tracks user activity and captures screenshots when the user is actively working. Uses a single `activity` table and `screenshots` table for simple data management.

## 🎯 What It Does

- **Activity Tracking**: Records when you're actively working on applications
- **Browser Tracking**: Captures URLs and page titles from browsers
- **Screenshot Capture**: Takes periodic screenshots when you're active
- **Idle Detection**: Only tracks when you're actually using the computer
- **Simple Database**: Just 2 tables - `activity` and `screenshots`

## 📊 Database Structure

### `activity` table:
- `app_name` - Application being used (chrome.exe, notepad.exe, etc.)
- `window_title` - Title of the active window
- `browser_url` - URL if it's a browser (optional)
- `browser_title` - Page title if it's a browser (optional)
- `start_time` / `end_time` - When the activity occurred
- `duration_seconds` - How long you used the application
- `is_active` - Whether user was actively working (not idle)

### `screenshots` table:
- `filename` - Screenshot filename with timestamp
- `file_path` - Full path to screenshot file
- `file_size` - Size of screenshot file
- `captured_at` - When screenshot was taken

## 🚀 Quick Setup

### 1. Prerequisites

- **MySQL Server** (any version 5.7+)
- **Go 1.21+** for building the application
- **Windows 10/11**

### 2. Database Setup

```sql
-- Create database and tables
mysql -u root -p < D:\desktime\tracker\setup_database.sql
```

### 3. Build and Run

```bash
cd D:\desktime\tracker

# Download dependencies
go mod tidy

# Build application  
go build -o tracker.exe cmd/tracker/main.go

# Run tracker
./tracker.exe
```

## ⚙️ Configuration

Edit `config/config.yaml`:

```yaml
tracking:
  activity_interval: 1          # Check every second
  idle_timeout: 180            # 3 minutes idle timeout  
  enable_screenshots: true      # Enable screenshots
  screenshot_interval: 300      # Screenshot every 5 minutes

database:
  host: "localhost"
  user: "root"
  password: ""                 # Your MySQL password
  database: "desktime"
```

## 📈 Viewing Your Data

### Recent Activity
```sql
SELECT app_name, window_title, browser_url, start_time, duration_seconds 
FROM activity 
ORDER BY start_time DESC 
LIMIT 10;
```

### Daily Summary by App
```sql
SELECT * FROM daily_activity_summary 
WHERE activity_date = CURDATE()
ORDER BY total_seconds DESC;
```

### Today's Screenshots
```sql
SELECT filename, captured_at, file_size
FROM screenshots
WHERE DATE(captured_at) = CURDATE()
ORDER BY captured_at DESC;
```

### Hourly Activity Pattern
```sql
SELECT * FROM hourly_activity 
WHERE activity_date = CURDATE()
ORDER BY hour;
```

## 📁 File Structure

```
tracker/
├── cmd/tracker/main.go          # Application entry point
├── internal/
│   ├── storage/database.go      # MySQL operations (simplified)
│   ├── models/models.go         # Data models (Activity + Screenshot)
│   ├── activity/tracker.go     # Activity tracking logic
│   ├── capture/screenshot.go   # Screenshot capture
│   └── config/config.go        # Configuration management
├── config/config.yaml           # Configuration file
└── screenshots/                # Screenshot files organized by date
```

## 💡 Key Features

- **Single Activity Table**: All activity stored in one simple table
- **Screenshot Integration**: Files stored in folders, metadata in database
- **Active User Detection**: Only tracks when user is working (not idle)
- **Browser URL Tracking**: Captures URLs and page titles automatically
- **Easy Queries**: Simple database structure for easy reporting
- **Automated Screenshots**: Periodic captures with timestamp filenames

## 🔧 How It Works

1. **Activity Detection**: Checks active window every second
2. **Idle Detection**: Stops tracking if no input for 3 minutes  
3. **Session Tracking**: Groups continuous activity into sessions
4. **Screenshot Capture**: Takes screenshots every 5 minutes when active
5. **Database Storage**: Saves activity to `activity` table, screenshots to `screenshots` table

## 📝 Sample Data

After running for a while, your `activity` table will look like:
```
| app_name    | window_title           | browser_url          | duration_seconds |
|-------------|------------------------|---------------------|-----------------|
| chrome.exe  | GitHub - Google Chrome | https://github.com  | 1250           |
| code.exe    | main.go - Visual Studio Code |              | 890            |
| notepad.exe | Document1 - Notepad    |                     | 145            |
```

Screenshots table:
```
| filename                        | captured_at         | file_size |
|--------------------------------|--------------------|---------| 
| screenshot_2024-01-15_14-30-00.png | 2024-01-15 14:30:00 | 245760   |
| screenshot_2024-01-15_14-35-00.png | 2024-01-15 14:35:00 | 267543   |
```

## 🎛️ PHP Endpoint (Optional)

If you want to use the PHP collection endpoint:

1. **Setup XAMPP** or similar PHP server
2. **Copy** `collect_data.php` to `C:\xampp\htdocs\desktime\` folder
3. **Enable** API in config: `api.enabled: true`
4. **Test** endpoint: `http://localhost/desktime/collect_data.php?stats`

The Go application can work with **direct MySQL** (recommended) or through the **PHP endpoint**.

## 🚀 Ready to Use

This simplified version focuses on the core functionality:
- ✅ **Single activity table** for all user activity
- ✅ **Screenshot capture** with file storage and database metadata
- ✅ **Active user detection** (no idle time tracking)
- ✅ **Browser URL tracking** for web activity
- ✅ **Simple setup** with minimal configuration
- ✅ **Easy querying** with straightforward database structure

Perfect for tracking your daily computer usage and productivity patterns!