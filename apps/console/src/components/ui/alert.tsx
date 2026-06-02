import * as React from "react";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

// Variant colors apply to the BORDER and the ICON only. Text uses
// text-foreground (the theme-aware primary text color) so the alert remains
// readable on both light and dark themes.
//
// Reasoning: the previous design used text-destructive-foreground /
// text-warning-foreground / text-success-foreground, which are *contrast*
// colors for the matching solid background. Combined with a translucent
// bg-*-/10 (a tinted version of the surface), the foreground color ended up
// being the wrong contrast — e.g. warning-foreground is black in the dark
// theme but is overlaid on a near-black card, making the text invisible.
const alertVariants = cva(
  "relative w-full rounded-lg border p-4 text-foreground [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&>svg+div]:translate-y-[-3px] [&>svg~*]:pl-7",
  {
    variants: {
      variant: {
        default: "bg-card text-card-foreground border-border [&>svg]:text-foreground",
        destructive: "border-destructive/30 bg-destructive/10 [&>svg]:text-destructive",
        warning: "border-warning/30 bg-warning/10 [&>svg]:text-warning",
        success: "border-success/30 bg-success/10 [&>svg]:text-success",
      },
    },
    defaultVariants: { variant: "default" },
  },
);

const Alert = React.forwardRef<HTMLDivElement, React.HTMLAttributes<HTMLDivElement> & VariantProps<typeof alertVariants>>(
  ({ className, variant, ...props }, ref) => (
    <div ref={ref} role="alert" className={cn(alertVariants({ variant }), className)} {...props} />
  ),
);
Alert.displayName = "Alert";

const AlertTitle = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLHeadingElement>>(
  ({ className, ...props }, ref) => (
    <h5 ref={ref} className={cn("mb-1 font-medium leading-none tracking-tight", className)} {...props} />
  ),
);
AlertTitle.displayName = "AlertTitle";

const AlertDescription = React.forwardRef<HTMLParagraphElement, React.HTMLAttributes<HTMLParagraphElement>>(
  ({ className, ...props }, ref) => (
    <div ref={ref} className={cn("text-sm [&_p]:leading-relaxed", className)} {...props} />
  ),
);
AlertDescription.displayName = "AlertDescription";

export { Alert, AlertTitle, AlertDescription };
