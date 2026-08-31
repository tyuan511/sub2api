/** @type {import('tailwindcss').Config} */
export default {
  content: ['./index.html', './src/**/*.{vue,js,ts,jsx,tsx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // FastVibe brand blue
        primary: {
          50: '#f1f5ff',
          100: '#dfe8ff',
          200: '#c7d7ff',
          300: '#9fbbff',
          400: '#7ea6ff',
          500: '#2f6fed',
          600: '#245bcc',
          700: '#1e4aa7',
          800: '#1d4087',
          900: '#1d376d',
          950: '#17274c'
        },
        // 辅助色 - 深蓝灰
        accent: {
          50: '#f8fafc',
          100: '#f1f5f9',
          200: '#e2e8f0',
          300: '#cbd5e1',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#0f172a',
          950: '#020617'
        },
        gray: {
          50: '#f6f8fb',
          100: '#eef2f7',
          200: '#cbd5e1',
          300: '#aab6c5',
          400: '#94a3b8',
          500: '#64748b',
          600: '#475569',
          700: '#334155',
          800: '#1e293b',
          900: '#111827',
          950: '#0c0f14'
        },
        // FastVibe dark surfaces
        dark: {
          50: '#f3f6fb',
          100: '#e8edf4',
          200: '#d6dee8',
          300: '#bac4d2',
          400: '#98a5b5',
          500: '#64748b',
          600: '#2b3545',
          700: '#1e2633',
          800: '#171d27',
          900: '#11161e',
          950: '#0c0f14'
        }
      },
      fontFamily: {
        sans: [
          'Noto Sans SC',
          'system-ui',
          '-apple-system',
          'BlinkMacSystemFont',
          'Segoe UI',
          'Roboto',
          'Helvetica Neue',
          'Arial',
          'PingFang SC',
          'Hiragino Sans GB',
          'Microsoft YaHei',
          'sans-serif'
        ],
        mono: ['DM Mono', 'SFMono-Regular', 'Menlo', 'Monaco', 'Consolas', 'monospace']
      },
      boxShadow: {
        glass: '0 14px 36px rgba(30, 64, 175, 0.08)',
        'glass-sm': '0 8px 24px rgba(30, 64, 175, 0.06)',
        glow: '0 10px 28px rgba(47, 111, 237, 0.18)',
        'glow-lg': '0 14px 36px rgba(47, 111, 237, 0.2)',
        card: 'none',
        'card-hover': '0 8px 24px rgba(30, 64, 175, 0.08)',
        'inner-glow': 'inset 0 1px 0 rgba(255, 255, 255, 0.1)'
      },
      backgroundImage: {
        'gradient-radial': 'radial-gradient(var(--tw-gradient-stops))',
        'gradient-primary': 'linear-gradient(135deg, #2f6fed 0%, #245bcc 100%)',
        'gradient-dark': 'linear-gradient(135deg, #1e293b 0%, #0f172a 100%)',
        'gradient-glass':
          'linear-gradient(135deg, rgba(255,255,255,0.1) 0%, rgba(255,255,255,0.05) 100%)',
        'mesh-gradient': 'none'
      },
      animation: {
        'fade-in': 'fadeIn 0.3s ease-out',
        'slide-up': 'slideUp 0.3s ease-out',
        'slide-down': 'slideDown 0.3s ease-out',
        'slide-in-right': 'slideInRight 0.3s ease-out',
        'scale-in': 'scaleIn 0.2s ease-out',
        'pulse-slow': 'pulse 3s cubic-bezier(0.4, 0, 0.6, 1) infinite',
        shimmer: 'shimmer 2s linear infinite',
        glow: 'glow 2s ease-in-out infinite alternate'
      },
      keyframes: {
        fadeIn: {
          '0%': { opacity: '0' },
          '100%': { opacity: '1' }
        },
        slideUp: {
          '0%': { opacity: '0', transform: 'translateY(10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideDown: {
          '0%': { opacity: '0', transform: 'translateY(-10px)' },
          '100%': { opacity: '1', transform: 'translateY(0)' }
        },
        slideInRight: {
          '0%': { opacity: '0', transform: 'translateX(20px)' },
          '100%': { opacity: '1', transform: 'translateX(0)' }
        },
        scaleIn: {
          '0%': { opacity: '0', transform: 'scale(0.95)' },
          '100%': { opacity: '1', transform: 'scale(1)' }
        },
        shimmer: {
          '0%': { backgroundPosition: '-200% 0' },
          '100%': { backgroundPosition: '200% 0' }
        },
        glow: {
          '0%': { boxShadow: '0 10px 24px rgba(47, 111, 237, 0.14)' },
          '100%': { boxShadow: '0 12px 30px rgba(47, 111, 237, 0.2)' }
        }
      },
      backdropBlur: {
        xs: '2px'
      },
      borderRadius: {
        DEFAULT: '4px',
        sm: '4px',
        md: '6px',
        lg: '6px',
        xl: '6px',
        '2xl': '6px',
        '3xl': '6px',
        '4xl': '2rem'
      }
    }
  },
  plugins: []
}
