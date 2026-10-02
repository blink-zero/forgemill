import * as React from "react";
import { Slot } from "@radix-ui/react-slot";
import { cva, type VariantProps } from "class-variance-authority";
import { cn } from "@/lib/utils";

/*
  Button variants, with destructive actions deliberately split in two:

  - `destructive` is what every delete / destroy / clear button in the app
    uses at rest. It is *outlined and tinted*, never a solid red block, so a
    high-risk control can't be mistaken for the page's primary action and an
    accidental click lands on a confirmation step rather than on the action.
  - `danger` is the solid red fill, reserved for the final commit button
    inside a confirmation (ConfirmDialog, typed-name confirms). It only ever
    appears after the user has already said "yes, this one".
*/
const buttonVariants = cva(
  "inline-flex items-center justify-center gap-1.5 whitespace-nowrap rounded-md text-13 font-medium transition-[background-color,border-color,color,box-shadow,transform] duration-150 focus-visible:outline-hidden focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background disabled:pointer-events-none disabled:opacity-50 active:translate-y-px [&>svg]:shrink-0",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground edge-highlight shadow-xs hover:bg-primary/90",
        destructive:
          "border border-destructive/40 bg-destructive/6 text-destructive hover:bg-destructive/12 hover:border-destructive/60",
        danger: "bg-destructive text-destructive-foreground edge-highlight shadow-xs hover:bg-destructive/90",
        outline: "border border-border bg-card text-foreground shadow-xs hover:bg-muted hover:border-input",
        secondary: "bg-secondary text-secondary-foreground hover:bg-accent",
        ghost: "text-muted-foreground hover:bg-accent hover:text-foreground",
        link: "text-primary underline-offset-4 hover:underline",
      },
      size: {
        default: "h-9 px-3.5",
        sm: "h-8 px-3 text-[12.5px]",
        lg: "h-10 px-6 text-sm",
        icon: "h-9 w-9",
      },
    },
    defaultVariants: {
      variant: "default",
      size: "default",
    },
  }
);

export interface ButtonProps
  extends React.ButtonHTMLAttributes<HTMLButtonElement>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

const Button = React.forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className, variant, size, asChild = false, ...props }, ref) => {
    const Comp = asChild ? Slot : "button";
    return (
      <Comp className={cn(buttonVariants({ variant, size, className }))} ref={ref} {...props} />
    );
  }
);
Button.displayName = "Button";

export { Button, buttonVariants };
