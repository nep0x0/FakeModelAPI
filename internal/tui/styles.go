package tui

import "github.com/charmbracelet/lipgloss"

var (
    Blue      = lipgloss.Color("#5FAFFF")
    White     = lipgloss.Color("#FFFFFF")
    Gray      = lipgloss.Color("#8A8A8A")
    FaintGray = lipgloss.Color("#6E6E6E")
    DarkGray  = lipgloss.Color("#4A4A4A")
    RedDot    = lipgloss.Color("#FF5F5F")
    GreenDot  = lipgloss.Color("#5FFF87")
    Orange    = lipgloss.Color("#FFA500")
)

var (
    LogoLightStyle = lipgloss.NewStyle().Foreground(White).Bold(true)
    LogoDarkStyle  = lipgloss.NewStyle().Foreground(FaintGray)

    ModeBuildStyle    = lipgloss.NewStyle().Foreground(Blue).Bold(true)
    ModeProviderStyle = lipgloss.NewStyle().Foreground(White)
    ModeEndpointStyle = lipgloss.NewStyle().Foreground(Gray)
    ModeMaxStyle      = lipgloss.NewStyle().Foreground(Orange).Bold(true)

    InputBoxStyle = lipgloss.NewStyle().
        Foreground(Gray).
        Border(lipgloss.NormalBorder()).
        BorderLeft(true).
        BorderForeground(Blue).
        BorderLeftForeground(Blue).
        Padding(0, 1)

    SlashSelectedStyle   = lipgloss.NewStyle().Foreground(White).Bold(true)
    SlashSuggestionStyle = lipgloss.NewStyle().Foreground(Gray)

    StatusBarStyle    = lipgloss.NewStyle().Foreground(FaintGray)
    StatusDotOnStyle  = lipgloss.NewStyle().Foreground(GreenDot)
    StatusDotOffStyle = lipgloss.NewStyle().Foreground(RedDot)
    VersionStyle      = lipgloss.NewStyle().Foreground(FaintGray)

    // Missing styles restored as TRANSPARENT (foreground only)
    TipDotStyle       = lipgloss.NewStyle().Foreground(Blue).Bold(true)
    UserMsgStyle      = lipgloss.NewStyle().Foreground(White).Bold(true)
    AssistantMsgStyle = lipgloss.NewStyle().Foreground(Gray)
)
