/**
 * The icon registry — generated, do not edit by hand.
 *
 * unplugin-icons resolves each import at build time and inlines the SVG, so a
 * screen ships only the glyphs it renders. That resolution is static, which is
 * why this file exists: the navigation model stores icon *names* as strings, and
 * a string cannot be an import. Mapping them here keeps the dynamic lookup
 * without falling back to Iconify's runtime API, which would fetch every glyph
 * over the network on first paint.
 *
 * Regenerate with scratchpad/genicons.mjs after adding a screen that uses a new
 * glyph. Names that do not exist in @iconify-json/material-symbols are dropped
 * rather than aliased to something similar, so a typo renders nothing and is
 * visible in review.
 */
import type { Component } from "vue";

import IAccountTree from "~icons/material-symbols/account-tree";
import IAddCircle from "~icons/material-symbols/add-circle";
import IAddToQueue from "~icons/material-symbols/add-to-queue";
import IAlternateEmail from "~icons/material-symbols/alternate-email";
import IAnalytics from "~icons/material-symbols/analytics";
import IApi from "~icons/material-symbols/api";
import IApps from "~icons/material-symbols/apps";
import IArrowBack from "~icons/material-symbols/arrow-back";
import IArrowDropDown from "~icons/material-symbols/arrow-drop-down";
import IArrowForward from "~icons/material-symbols/arrow-forward";
import IArrowRight from "~icons/material-symbols/arrow-right";
import IArrowUpward from "~icons/material-symbols/arrow-upward";
import IAutoAwesome from "~icons/material-symbols/auto-awesome";
import IBackup from "~icons/material-symbols/backup";
import IBarChart from "~icons/material-symbols/bar-chart";
import IBedtime from "~icons/material-symbols/bedtime";
import IBlock from "~icons/material-symbols/block";
import IBolt from "~icons/material-symbols/bolt";
import IBugReport from "~icons/material-symbols/bug-report";
import ICalculate from "~icons/material-symbols/calculate";
import ICallSplit from "~icons/material-symbols/call-split";
import ICardMembership from "~icons/material-symbols/card-membership";
import ICheck from "~icons/material-symbols/check";
import ICheckBox from "~icons/material-symbols/check-box";
import ICheckBoxOutlineBlank from "~icons/material-symbols/check-box-outline-blank";
import ICheckCircle from "~icons/material-symbols/check-circle";
import IChevronRight from "~icons/material-symbols/chevron-right";
import ICircle from "~icons/material-symbols/circle";
import ICleaningServices from "~icons/material-symbols/cleaning-services";
import IClose from "~icons/material-symbols/close";
import ICloud from "~icons/material-symbols/cloud";
import ICloudOff from "~icons/material-symbols/cloud-off";
import ICloudSync from "~icons/material-symbols/cloud-sync";
import ICloudUpload from "~icons/material-symbols/cloud-upload";
import ICode from "~icons/material-symbols/code";
import ICommit from "~icons/material-symbols/commit";
import IConstruction from "~icons/material-symbols/construction";
import IContentCopy from "~icons/material-symbols/content-copy";
import ICreateNewFolder from "~icons/material-symbols/create-new-folder";
import IDashboard from "~icons/material-symbols/dashboard";
import IDatabase from "~icons/material-symbols/database";
import IDelete from "~icons/material-symbols/delete";
import IDeployedCode from "~icons/material-symbols/deployed-code";
import IDescription from "~icons/material-symbols/description";
import IDesktopWindows from "~icons/material-symbols/desktop-windows";
import IDeveloperBoard from "~icons/material-symbols/developer-board";
import IDns from "~icons/material-symbols/dns";
import IDownload from "~icons/material-symbols/download";
import IDriveFileMove from "~icons/material-symbols/drive-file-move";
import IDriveFileRenameOutline from "~icons/material-symbols/drive-file-rename-outline";
import IError from "~icons/material-symbols/error";
import IExpandLess from "~icons/material-symbols/expand-less";
import IExpandMore from "~icons/material-symbols/expand-more";
import IExtension from "~icons/material-symbols/extension";
import IFolder from "~icons/material-symbols/folder";
import IFolderOpen from "~icons/material-symbols/folder-open";
import IFolderZip from "~icons/material-symbols/folder-zip";
import IGppBad from "~icons/material-symbols/gpp-bad";
import IGppMaybe from "~icons/material-symbols/gpp-maybe";
import IGridView from "~icons/material-symbols/grid-view";
import IGroup from "~icons/material-symbols/group";
import IHelp from "~icons/material-symbols/help";
import IHistory from "~icons/material-symbols/history";
import IHome from "~icons/material-symbols/home";
import IImage from "~icons/material-symbols/image";
import IInfo from "~icons/material-symbols/info";
import IInventory2 from "~icons/material-symbols/inventory-2";
import IJavascript from "~icons/material-symbols/javascript";
import IKey from "~icons/material-symbols/key";
import ILan from "~icons/material-symbols/lan";
import ILanguage from "~icons/material-symbols/language";
import ILinkOff from "~icons/material-symbols/link-off";
import IList from "~icons/material-symbols/list";
import ILocalFireDepartment from "~icons/material-symbols/local-fire-department";
import ILocalParking from "~icons/material-symbols/local-parking";
import ILock from "~icons/material-symbols/lock";
import ILockClock from "~icons/material-symbols/lock-clock";
import ILockOpen from "~icons/material-symbols/lock-open";
import ILogin from "~icons/material-symbols/login";
import ILogout from "~icons/material-symbols/logout";
import IMail from "~icons/material-symbols/mail";
import IMemory from "~icons/material-symbols/memory";
import IMonitorHeart from "~icons/material-symbols/monitor-heart";
import IMonitoring from "~icons/material-symbols/monitoring";
import IMoreHoriz from "~icons/material-symbols/more-horiz";
import IMove from "~icons/material-symbols/move";
import INoteAdd from "~icons/material-symbols/note-add";
import INotifications from "~icons/material-symbols/notifications";
import INotificationsActive from "~icons/material-symbols/notifications-active";
import IOpenInNew from "~icons/material-symbols/open-in-new";
import IOverview from "~icons/material-symbols/overview";
import IPhonelinkLock from "~icons/material-symbols/phonelink-lock";
import IPhp from "~icons/material-symbols/php";
import IPlayCircle from "~icons/material-symbols/play-circle";
import IPolicy from "~icons/material-symbols/policy";
import IProgressActivity from "~icons/material-symbols/progress-activity";
import IPublic from "~icons/material-symbols/public";
import IReceiptLong from "~icons/material-symbols/receipt-long";
import IRestartAlt from "~icons/material-symbols/restart-alt";
import ISchedule from "~icons/material-symbols/schedule";
import IScheduleSend from "~icons/material-symbols/schedule-send";
import ISearch from "~icons/material-symbols/search";
import ISearchOff from "~icons/material-symbols/search-off";
import ISecurity from "~icons/material-symbols/security";
import ISensors from "~icons/material-symbols/sensors";
import ISettings from "~icons/material-symbols/settings";
import ISettingsEthernet from "~icons/material-symbols/settings-ethernet";
import IShield from "~icons/material-symbols/shield";
import IShieldLock from "~icons/material-symbols/shield-lock";
import ISmartToy from "~icons/material-symbols/smart-toy";
import ISoap from "~icons/material-symbols/soap";
import ISpaceDashboard from "~icons/material-symbols/space-dashboard";
import ISpeed from "~icons/material-symbols/speed";
import IStacks from "~icons/material-symbols/stacks";
import IStorage from "~icons/material-symbols/storage";
import IStream from "~icons/material-symbols/stream";
import ISwapHoriz from "~icons/material-symbols/swap-horiz";
import ISwapVert from "~icons/material-symbols/swap-vert";
import ISyncAlt from "~icons/material-symbols/sync-alt";
import ISyncProblem from "~icons/material-symbols/sync-problem";
import ISystemUpdateAlt from "~icons/material-symbols/system-update-alt";
import ITableView from "~icons/material-symbols/table-view";
import ITerminal from "~icons/material-symbols/terminal";
import ITravelExplore from "~icons/material-symbols/travel-explore";
import ITrendingUp from "~icons/material-symbols/trending-up";
import ITune from "~icons/material-symbols/tune";
import IUpdate from "~icons/material-symbols/update";
import IUpload from "~icons/material-symbols/upload";
import IUploadFile from "~icons/material-symbols/upload-file";
import IVerified from "~icons/material-symbols/verified";
import IVerifiedUser from "~icons/material-symbols/verified-user";
import IViewTimeline from "~icons/material-symbols/view-timeline";
import IVisibility from "~icons/material-symbols/visibility";
import IVpnKey from "~icons/material-symbols/vpn-key";
import IWarning from "~icons/material-symbols/warning";
import IWidgets from "~icons/material-symbols/widgets";

export const ICONS: Readonly<Record<string, Component>> = {
  "account-tree": IAccountTree,
  "add-circle": IAddCircle,
  "add-to-queue": IAddToQueue,
  "alternate-email": IAlternateEmail,
  "analytics": IAnalytics,
  "api": IApi,
  "apps": IApps,
  "arrow-back": IArrowBack,
  "arrow-drop-down": IArrowDropDown,
  "arrow-forward": IArrowForward,
  "arrow-right": IArrowRight,
  "arrow-upward": IArrowUpward,
  "auto-awesome": IAutoAwesome,
  "backup": IBackup,
  "bar-chart": IBarChart,
  "bedtime": IBedtime,
  "block": IBlock,
  "bolt": IBolt,
  "bug-report": IBugReport,
  "calculate": ICalculate,
  "call-split": ICallSplit,
  "card-membership": ICardMembership,
  "check": ICheck,
  "check-box": ICheckBox,
  "check-box-outline-blank": ICheckBoxOutlineBlank,
  "check-circle": ICheckCircle,
  "chevron-right": IChevronRight,
  "circle": ICircle,
  "cleaning-services": ICleaningServices,
  "close": IClose,
  "cloud": ICloud,
  "cloud-off": ICloudOff,
  "cloud-sync": ICloudSync,
  "cloud-upload": ICloudUpload,
  "code": ICode,
  "commit": ICommit,
  "construction": IConstruction,
  "content-copy": IContentCopy,
  "create-new-folder": ICreateNewFolder,
  "dashboard": IDashboard,
  "database": IDatabase,
  "delete": IDelete,
  "deployed-code": IDeployedCode,
  "description": IDescription,
  "desktop-windows": IDesktopWindows,
  "developer-board": IDeveloperBoard,
  "dns": IDns,
  "download": IDownload,
  "drive-file-move": IDriveFileMove,
  "drive-file-rename-outline": IDriveFileRenameOutline,
  "error": IError,
  "expand-less": IExpandLess,
  "expand-more": IExpandMore,
  "extension": IExtension,
  "folder": IFolder,
  "folder-open": IFolderOpen,
  "folder-zip": IFolderZip,
  "gpp-bad": IGppBad,
  "gpp-maybe": IGppMaybe,
  "grid-view": IGridView,
  "group": IGroup,
  "help": IHelp,
  "history": IHistory,
  "home": IHome,
  "image": IImage,
  "info": IInfo,
  "inventory-2": IInventory2,
  "javascript": IJavascript,
  "key": IKey,
  "lan": ILan,
  "language": ILanguage,
  "link-off": ILinkOff,
  "list": IList,
  "local-fire-department": ILocalFireDepartment,
  "local-parking": ILocalParking,
  "lock": ILock,
  "lock-clock": ILockClock,
  "lock-open": ILockOpen,
  "login": ILogin,
  "logout": ILogout,
  "mail": IMail,
  "memory": IMemory,
  "monitor-heart": IMonitorHeart,
  "monitoring": IMonitoring,
  "more-horiz": IMoreHoriz,
  "move": IMove,
  "note-add": INoteAdd,
  "notifications": INotifications,
  "notifications-active": INotificationsActive,
  "open-in-new": IOpenInNew,
  "overview": IOverview,
  "phonelink-lock": IPhonelinkLock,
  "php": IPhp,
  "play-circle": IPlayCircle,
  "policy": IPolicy,
  "progress-activity": IProgressActivity,
  "public": IPublic,
  "receipt-long": IReceiptLong,
  "restart-alt": IRestartAlt,
  "schedule": ISchedule,
  "schedule-send": IScheduleSend,
  "search": ISearch,
  "search-off": ISearchOff,
  "security": ISecurity,
  "sensors": ISensors,
  "settings": ISettings,
  "settings-ethernet": ISettingsEthernet,
  "shield": IShield,
  "shield-lock": IShieldLock,
  "smart-toy": ISmartToy,
  "soap": ISoap,
  "space-dashboard": ISpaceDashboard,
  "speed": ISpeed,
  "stacks": IStacks,
  "storage": IStorage,
  "stream": IStream,
  "swap-horiz": ISwapHoriz,
  "swap-vert": ISwapVert,
  "sync-alt": ISyncAlt,
  "sync-problem": ISyncProblem,
  "system-update-alt": ISystemUpdateAlt,
  "table-view": ITableView,
  "terminal": ITerminal,
  "travel-explore": ITravelExplore,
  "trending-up": ITrendingUp,
  "tune": ITune,
  "update": IUpdate,
  "upload": IUpload,
  "upload-file": IUploadFile,
  "verified": IVerified,
  "verified-user": IVerifiedUser,
  "view-timeline": IViewTimeline,
  "visibility": IVisibility,
  "vpn-key": IVpnKey,
  "warning": IWarning,
  "widgets": IWidgets,
};

export type IconName = keyof typeof ICONS;

/** Whether a name will render. Lets a caller fall back instead of rendering nothing. */
export function hasIcon(name: string): boolean {
  return name in ICONS;
}
